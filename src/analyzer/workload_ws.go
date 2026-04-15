package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

var upgrader = websocket.Upgrader{
	CheckOrigin:  func(r *http.Request) bool { return true },
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

// RegisterWSRoutes registers the WebSocket-based routes (logs, exec)
// and the write-action REST routes on the supplied mux.
func (h *WorkloadHandler) RegisterWSRoutes(mux *http.ServeMux, restCfg *rest.Config, cfg wsConfig) {
	mux.HandleFunc("GET /api/v1/k8s/pods/{namespace}/{name}/logs", h.handlePodLogsWS)

	if cfg.ExecEnabled {
		h.restConfig = restCfg
		h.execAllowedCmds = cfg.ExecAllowedCommands
		mux.HandleFunc("GET /api/v1/k8s/pods/{namespace}/{name}/exec", h.handlePodExec)
	}

	if cfg.WriteEnabled {
		h.protectedNS = cfg.ProtectedNamespaces
		mux.HandleFunc("POST /api/v1/k8s/actions/scale", h.handleScale)
		mux.HandleFunc("POST /api/v1/k8s/actions/restart", h.handleRestart)
		mux.HandleFunc("POST /api/v1/k8s/actions/delete", h.handleDelete)
	}

	// Capabilities probe so the frontend knows what's enabled
	mux.HandleFunc("GET /api/v1/k8s/capabilities", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]bool{
			"exec":         cfg.ExecEnabled,
			"writeActions": cfg.WriteEnabled,
		})
	})
}

type wsConfig struct {
	ExecEnabled         bool
	ExecAllowedCommands []string
	WriteEnabled        bool
	ProtectedNamespaces []string
}

// --- Pod log streaming (WebSocket) -------------------------------------------

func (h *WorkloadHandler) handlePodLogsWS(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("namespace")
	name := r.PathValue("name")
	container := r.URL.Query().Get("container")
	follow := r.URL.Query().Get("follow") != "false"
	tailStr := r.URL.Query().Get("tail")

	var tailLines *int64
	if tailStr != "" {
		if n, err := strconv.ParseInt(tailStr, 10, 64); err == nil {
			tailLines = &n
		}
	} else {
		def := int64(200)
		tailLines = &def
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("ws upgrade failed")
		return
	}
	defer conn.Close()

	opts := &corev1.PodLogOptions{
		Follow:    follow,
		TailLines: tailLines,
	}
	if container != "" {
		opts.Container = container
	}

	stream, err := h.clientset.CoreV1().Pods(ns).GetLogs(name, opts).Stream(r.Context())
	if err != nil {
		conn.WriteJSON(map[string]string{"error": err.Error()})
		return
	}
	defer stream.Close()

	// Heartbeat: send periodic proof-of-life so the frontend knows the
	// stream is still connected even when the pod isn't writing logs.
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				if err := conn.WriteJSON(map[string]string{
					"type": "heartbeat",
					"ts":   time.Now().Format(time.RFC3339),
				}); err != nil {
					return
				}
			}
		}
	}()

	// Read lines and send over WS
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for scanner.Scan() {
		if err := conn.WriteMessage(websocket.TextMessage, scanner.Bytes()); err != nil {
			break // client disconnected
		}
	}
	if err := scanner.Err(); err != nil {
		conn.WriteJSON(map[string]string{"error": err.Error()})
	}
	<-heartbeatDone
}

// --- Pod exec (WebSocket) ----------------------------------------------------

func (h *WorkloadHandler) handlePodExec(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("namespace")
	name := r.PathValue("name")
	container := r.URL.Query().Get("container")
	command := r.URL.Query().Get("command")

	if command == "" {
		command = "/bin/sh"
	}

	// Validate command against allowlist
	if len(h.execAllowedCmds) > 0 {
		allowed := false
		for _, c := range h.execAllowedCmds {
			if c == command {
				allowed = true
				break
			}
		}
		if !allowed {
			http.Error(w, fmt.Sprintf("command %q not in allowed list", command), http.StatusForbidden)
			return
		}
	}

	// Check protected namespaces
	if h.isProtected(ns) {
		http.Error(w, fmt.Sprintf("namespace %q is protected", ns), http.StatusForbidden)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("ws upgrade failed for exec")
		return
	}
	defer conn.Close()

	// Write audit log
	h.writeAudit("system", "pod.exec", map[string]any{
		"namespace": ns, "pod": name, "container": container, "command": command,
	}, nil, "started")

	execReq := h.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(name).
		Namespace(ns).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   []string{command},
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(h.restConfig, "POST", execReq.URL())
	if err != nil {
		conn.WriteJSON(map[string]string{"error": err.Error()})
		return
	}

	wsStream := &wsStreamAdapter{conn: conn}

	err = exec.StreamWithContext(r.Context(), remotecommand.StreamOptions{
		Stdin:  wsStream,
		Stdout: wsStream,
		Stderr: wsStream,
		Tty:    true,
	})
	if err != nil {
		conn.WriteJSON(map[string]string{"error": err.Error()})
	}
}

// wsStreamAdapter adapts a WebSocket connection to io.Reader/io.Writer
// for use with remotecommand.StreamOptions.
type wsStreamAdapter struct {
	conn   *websocket.Conn
	reader io.Reader
}

func (ws *wsStreamAdapter) Read(p []byte) (int, error) {
	if ws.reader == nil {
		_, r, err := ws.conn.NextReader()
		if err != nil {
			return 0, err
		}
		ws.reader = r
	}
	n, err := ws.reader.Read(p)
	if err == io.EOF {
		ws.reader = nil
		return n, nil
	}
	return n, err
}

func (ws *wsStreamAdapter) Write(p []byte) (int, error) {
	err := ws.conn.WriteMessage(websocket.BinaryMessage, p)
	return len(p), err
}

// --- Write actions -----------------------------------------------------------

type actionRequest struct {
	Kind      string `json:"kind"`
	Group     string `json:"group"`
	Version   string `json:"version"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Replicas  *int32 `json:"replicas,omitempty"`
}

func (h *WorkloadHandler) handleScale(w http.ResponseWriter, r *http.Request) {
	var req actionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Replicas == nil {
		http.Error(w, "replicas required", http.StatusBadRequest)
		return
	}
	if h.isProtected(req.Namespace) {
		http.Error(w, fmt.Sprintf("namespace %q is protected", req.Namespace), http.StatusForbidden)
		return
	}

	ctx := r.Context()
	scale, err := h.clientset.AppsV1().Deployments(req.Namespace).GetScale(ctx, req.Name, metav1.GetOptions{})
	if err != nil {
		http.Error(w, err.Error(), statusFromK8sErr(err))
		return
	}
	scale.Spec.Replicas = *req.Replicas
	_, err = h.clientset.AppsV1().Deployments(req.Namespace).UpdateScale(ctx, req.Name, scale, metav1.UpdateOptions{})
	if err != nil {
		http.Error(w, err.Error(), statusFromK8sErr(err))
		return
	}

	h.writeAudit("user", "workload.scale", map[string]any{
		"kind": req.Kind, "namespace": req.Namespace, "name": req.Name,
	}, map[string]any{"replicas": *req.Replicas}, "success")

	writeJSON(w, map[string]any{"status": "scaled", "replicas": *req.Replicas})
}

func (h *WorkloadHandler) handleRestart(w http.ResponseWriter, r *http.Request) {
	var req actionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if h.isProtected(req.Namespace) {
		http.Error(w, fmt.Sprintf("namespace %q is protected", req.Namespace), http.StatusForbidden)
		return
	}

	ctx := r.Context()
	deploy, err := h.clientset.AppsV1().Deployments(req.Namespace).Get(ctx, req.Name, metav1.GetOptions{})
	if err != nil {
		http.Error(w, err.Error(), statusFromK8sErr(err))
		return
	}

	if deploy.Spec.Template.Annotations == nil {
		deploy.Spec.Template.Annotations = map[string]string{}
	}
	deploy.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

	_, err = h.clientset.AppsV1().Deployments(req.Namespace).Update(ctx, deploy, metav1.UpdateOptions{})
	if err != nil {
		http.Error(w, err.Error(), statusFromK8sErr(err))
		return
	}

	h.writeAudit("user", "workload.restart", map[string]any{
		"kind": req.Kind, "namespace": req.Namespace, "name": req.Name,
	}, nil, "success")

	writeJSON(w, map[string]any{"status": "restarted"})
}

func (h *WorkloadHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	var req actionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if h.isProtected(req.Namespace) {
		http.Error(w, fmt.Sprintf("namespace %q is protected", req.Namespace), http.StatusForbidden)
		return
	}

	ctx := r.Context()
	gvr := gvrFromAction(req)
	var err error
	if req.Namespace != "" {
		err = h.dynamic.Resource(gvr).Namespace(req.Namespace).Delete(ctx, req.Name, metav1.DeleteOptions{})
	} else {
		err = h.dynamic.Resource(gvr).Delete(ctx, req.Name, metav1.DeleteOptions{})
	}
	if err != nil {
		http.Error(w, err.Error(), statusFromK8sErr(err))
		return
	}

	h.writeAudit("user", "workload.delete", map[string]any{
		"kind": req.Kind, "namespace": req.Namespace, "name": req.Name,
	}, nil, "success")

	writeJSON(w, map[string]any{"status": "deleted"})
}

// --- Helpers -----------------------------------------------------------------

func (h *WorkloadHandler) isProtected(ns string) bool {
	for _, p := range h.protectedNS {
		if p == ns {
			return true
		}
	}
	return false
}

func (h *WorkloadHandler) writeAudit(actor, action string, target, request map[string]any, result string) {
	log.Info().
		Str("actor", actor).
		Str("action", action).
		Any("target", target).
		Any("request", request).
		Str("result", result).
		Msg("audit")
	// TODO: write to Postgres audit_log table when store is wired
}

func gvrFromAction(req actionRequest) schema.GroupVersionResource {
	group := req.Group
	if group == "core" {
		group = ""
	}
	return schema.GroupVersionResource{
		Group:    group,
		Version:  req.Version,
		Resource: req.Kind,
	}
}
