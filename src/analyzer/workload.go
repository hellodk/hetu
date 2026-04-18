package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"sigs.k8s.io/yaml"
)

// WorkloadHandler provides the /api/v1/k8s/* endpoints for the workload
// browser — listing, detail, YAML, and events for any K8s resource kind.
type WorkloadHandler struct {
	clientset       kubernetes.Interface
	dynamic         dynamic.Interface
	discovery       discovery.DiscoveryInterface
	restConfig      *rest.Config
	execAllowedCmds []string
	protectedNS     []string
}

// NewWorkloadHandler creates a handler from the supplied clients.
func NewWorkloadHandler(cs kubernetes.Interface, dyn dynamic.Interface, disc discovery.DiscoveryInterface) *WorkloadHandler {
	return &WorkloadHandler{clientset: cs, dynamic: dyn, discovery: disc}
}

// Clientset returns the underlying Kubernetes clientset for use by other components.
func (h *WorkloadHandler) Clientset() kubernetes.Interface { return h.clientset }

// RegisterRoutes adds all workload browser routes to the supplied mux.
func (h *WorkloadHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/k8s/resources", h.handleResources)
	mux.HandleFunc("GET /api/v1/k8s/namespaces", h.handleNamespaces)

	// Namespaced resources: list, detail, yaml, events
	mux.HandleFunc("GET /api/v1/k8s/ns/{namespace}/{group}/{version}/{kind}", h.handleList)
	mux.HandleFunc("GET /api/v1/k8s/ns/{namespace}/{group}/{version}/{kind}/{name}", h.handleDetail)
	mux.HandleFunc("GET /api/v1/k8s/ns/{namespace}/{group}/{version}/{kind}/{name}/yaml", h.handleYAML)
	mux.HandleFunc("GET /api/v1/k8s/ns/{namespace}/{group}/{version}/{kind}/{name}/events", h.handleEvents)
	mux.HandleFunc("GET /api/v1/k8s/ns/{namespace}/{group}/{version}/{kind}/{name}/pods", h.handleChildPods)

	// Cluster-scoped resources (prefixed with /cluster/ to avoid route conflicts)
	mux.HandleFunc("GET /api/v1/k8s/cluster/{group}/{version}/{kind}", h.handleListCluster)
	mux.HandleFunc("GET /api/v1/k8s/cluster/{group}/{version}/{kind}/{name}", h.handleDetailCluster)
	mux.HandleFunc("GET /api/v1/k8s/cluster/{group}/{version}/{kind}/{name}/yaml", h.handleYAMLCluster)
	mux.HandleFunc("GET /api/v1/k8s/cluster/{group}/{version}/{kind}/{name}/events", h.handleEventsCluster)
}

// --- Discovery ---------------------------------------------------------------

// ResourceGroup groups API resources by category for the sidebar.
type ResourceGroup struct {
	Name      string         `json:"name"`
	Resources []ResourceInfo `json:"resources"`
}

// ResourceInfo describes a single API resource.
type ResourceInfo struct {
	Group      string `json:"group"`
	Version    string `json:"version"`
	Kind       string `json:"kind"`
	Resource   string `json:"resource"` // plural name for URLs
	Namespaced bool   `json:"namespaced"`
}

func (h *WorkloadHandler) handleResources(w http.ResponseWriter, r *http.Request) {
	lists, err := h.discovery.ServerPreferredResources()
	if err != nil {
		// Partial results are common (some groups may fail); continue.
		log.Debug().Err(err).Msg("discovery partial error")
	}

	// Build a lookup of interesting resources grouped by category.
	groups := buildResourceGroups(lists)
	writeJSON(w, groups)
}

// category ordering for the sidebar.
var categoryMap = map[string]string{
	"pods":                     "Workloads",
	"deployments":              "Workloads",
	"statefulsets":             "Workloads",
	"daemonsets":               "Workloads",
	"replicasets":              "Workloads",
	"jobs":                     "Workloads",
	"cronjobs":                 "Workloads",
	"replicationcontrollers":   "Workloads",
	"services":                 "Service & Networking",
	"endpoints":                "Service & Networking",
	"endpointslices":           "Service & Networking",
	"ingresses":                "Service & Networking",
	"ingressclasses":           "Service & Networking",
	"networkpolicies":          "Service & Networking",
	"configmaps":               "Config & Storage",
	"secrets":                  "Config & Storage",
	"persistentvolumeclaims":   "Config & Storage",
	"persistentvolumes":        "Config & Storage",
	"storageclasses":           "Config & Storage",
	"serviceaccounts":          "RBAC",
	"roles":                    "RBAC",
	"rolebindings":             "RBAC",
	"clusterroles":             "RBAC",
	"clusterrolebindings":      "RBAC",
	"nodes":                    "Cluster",
	"namespaces":               "Cluster",
	"events":                   "Cluster",
	"resourcequotas":           "Cluster",
	"limitranges":              "Cluster",
	"leases":                   "Cluster",
	"priorityclasses":          "Cluster",
	"horizontalpodautoscalers": "Autoscaling",
	"poddisruptionbudgets":     "Autoscaling",
}

func buildResourceGroups(lists []*metav1.APIResourceList) []ResourceGroup {
	byCategory := map[string][]ResourceInfo{}
	for _, list := range lists {
		if list == nil {
			continue
		}
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			continue
		}
		for _, res := range list.APIResources {
			// Skip sub-resources (e.g. pods/log, pods/status).
			if strings.Contains(res.Name, "/") {
				continue
			}
			cat, ok := categoryMap[res.Name]
			if !ok {
				continue // only expose resources we explicitly categorize
			}
			group := gv.Group
			if group == "" {
				group = "core"
			}
			byCategory[cat] = append(byCategory[cat], ResourceInfo{
				Group:      group,
				Version:    gv.Version,
				Kind:       res.Kind,
				Resource:   res.Name,
				Namespaced: res.Namespaced,
			})
		}
	}

	order := []string{"Workloads", "Service & Networking", "Config & Storage", "RBAC", "Cluster", "Autoscaling"}
	var groups []ResourceGroup
	for _, name := range order {
		if items, ok := byCategory[name]; ok {
			sort.Slice(items, func(i, j int) bool { return items[i].Resource < items[j].Resource })
			groups = append(groups, ResourceGroup{Name: name, Resources: items})
		}
	}
	return groups
}

// --- Namespaces --------------------------------------------------------------

func (h *WorkloadHandler) handleNamespaces(w http.ResponseWriter, r *http.Request) {
	nsList, err := h.clientset.CoreV1().Namespaces().List(r.Context(), metav1.ListOptions{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	names := make([]string, 0, len(nsList.Items))
	for _, ns := range nsList.Items {
		names = append(names, ns.Name)
	}
	sort.Strings(names)
	writeJSON(w, map[string][]string{"namespaces": names})
}

// --- List (namespaced) -------------------------------------------------------

func (h *WorkloadHandler) handleList(w http.ResponseWriter, r *http.Request) {
	gvr := gvrFromRequest(r)
	ns := r.PathValue("namespace")
	h.listResources(w, r, gvr, ns)
}

func (h *WorkloadHandler) handleListCluster(w http.ResponseWriter, r *http.Request) {
	gvr := gvrFromRequest(r)
	h.listResources(w, r, gvr, "")
}

func (h *WorkloadHandler) listResources(w http.ResponseWriter, r *http.Request, gvr schema.GroupVersionResource, ns string) {
	opts := metav1.ListOptions{}
	if ls := r.URL.Query().Get("labelSelector"); ls != "" {
		opts.LabelSelector = ls
	}
	if fs := r.URL.Query().Get("fieldSelector"); fs != "" {
		opts.FieldSelector = fs
	}

	var list *unstructured.UnstructuredList
	var err error
	if ns != "" {
		list, err = h.dynamic.Resource(gvr).Namespace(ns).List(r.Context(), opts)
	} else {
		list, err = h.dynamic.Resource(gvr).List(r.Context(), opts)
	}
	if err != nil {
		http.Error(w, err.Error(), statusFromK8sErr(err))
		return
	}

	// Apply client-side name search if provided.
	search := strings.ToLower(r.URL.Query().Get("search"))
	items := list.Items
	if search != "" {
		filtered := items[:0]
		for _, item := range items {
			if strings.Contains(strings.ToLower(item.GetName()), search) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}

	// Build summary rows for the table.
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, summarize(item))
	}

	writeJSON(w, map[string]any{
		"kind":       gvr.Resource,
		"namespace":  ns,
		"totalCount": len(rows),
		"items":      rows,
	})
}

// --- Detail ------------------------------------------------------------------

func (h *WorkloadHandler) handleDetail(w http.ResponseWriter, r *http.Request) {
	gvr := gvrFromRequest(r)
	ns := r.PathValue("namespace")
	name := r.PathValue("name")
	h.getResource(w, r, gvr, ns, name)
}

func (h *WorkloadHandler) handleDetailCluster(w http.ResponseWriter, r *http.Request) {
	gvr := gvrFromRequest(r)
	name := r.PathValue("name")
	h.getResource(w, r, gvr, "", name)
}

func (h *WorkloadHandler) getResource(w http.ResponseWriter, r *http.Request, gvr schema.GroupVersionResource, ns, name string) {
	var obj *unstructured.Unstructured
	var err error
	if ns != "" {
		obj, err = h.dynamic.Resource(gvr).Namespace(ns).Get(r.Context(), name, metav1.GetOptions{})
	} else {
		obj, err = h.dynamic.Resource(gvr).Get(r.Context(), name, metav1.GetOptions{})
	}
	if err != nil {
		http.Error(w, err.Error(), statusFromK8sErr(err))
		return
	}
	writeJSON(w, obj.Object)
}

// --- YAML --------------------------------------------------------------------

func (h *WorkloadHandler) handleYAML(w http.ResponseWriter, r *http.Request) {
	gvr := gvrFromRequest(r)
	ns := r.PathValue("namespace")
	name := r.PathValue("name")
	h.getResourceYAML(w, r, gvr, ns, name)
}

func (h *WorkloadHandler) handleYAMLCluster(w http.ResponseWriter, r *http.Request) {
	gvr := gvrFromRequest(r)
	name := r.PathValue("name")
	h.getResourceYAML(w, r, gvr, "", name)
}

func (h *WorkloadHandler) getResourceYAML(w http.ResponseWriter, r *http.Request, gvr schema.GroupVersionResource, ns, name string) {
	var obj *unstructured.Unstructured
	var err error
	if ns != "" {
		obj, err = h.dynamic.Resource(gvr).Namespace(ns).Get(r.Context(), name, metav1.GetOptions{})
	} else {
		obj, err = h.dynamic.Resource(gvr).Get(r.Context(), name, metav1.GetOptions{})
	}
	if err != nil {
		http.Error(w, err.Error(), statusFromK8sErr(err))
		return
	}

	out, err := yaml.Marshal(obj.Object)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Write(out)
}

// --- Events ------------------------------------------------------------------

func (h *WorkloadHandler) handleEvents(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("namespace")
	name := r.PathValue("name")
	kind := r.PathValue("kind")
	h.getEvents(w, r, ns, name, kind)
}

func (h *WorkloadHandler) handleEventsCluster(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	kind := r.PathValue("kind")
	h.getEvents(w, r, "", name, kind)
}

func (h *WorkloadHandler) getEvents(w http.ResponseWriter, r *http.Request, ns, name, kind string) {
	// Query events matching this object. We filter only on name here —
	// kind comes in as the plural resource name ("pods") not the
	// singular Kind the K8s API expects ("Pod"), and downstream
	// displays typically don't need the extra narrowing. If name uniqueness
	// becomes an issue across kinds, extend this with a kindSingular map.
	_ = kind
	fs := fmt.Sprintf("involvedObject.name=%s", name)
	opts := metav1.ListOptions{FieldSelector: fs}
	var eventList interface{}
	var err error
	if ns != "" {
		eventList, err = h.clientset.CoreV1().Events(ns).List(r.Context(), opts)
	} else {
		eventList, err = h.clientset.CoreV1().Events("").List(r.Context(), opts)
	}
	if err != nil {
		http.Error(w, err.Error(), statusFromK8sErr(err))
		return
	}
	writeJSON(w, eventList)
}

// --- Child Pods --------------------------------------------------------------

// handleChildPods returns the pods owned by a parent workload (Deployment,
// StatefulSet, ReplicaSet, DaemonSet). It reads .spec.selector.matchLabels
// from the parent and lists pods matching that label selector.
func (h *WorkloadHandler) handleChildPods(w http.ResponseWriter, r *http.Request) {
	gvr := gvrFromRequest(r)
	ns := r.PathValue("namespace")
	name := r.PathValue("name")

	// 1. Get the parent resource via dynamic client.
	parent, err := h.dynamic.Resource(gvr).Namespace(ns).Get(r.Context(), name, metav1.GetOptions{})
	if err != nil {
		http.Error(w, err.Error(), statusFromK8sErr(err))
		return
	}

	// 2. Extract .spec.selector.matchLabels from the unstructured object.
	matchLabels, found, err := unstructured.NestedStringMap(parent.Object, "spec", "selector", "matchLabels")
	if err != nil || !found || len(matchLabels) == 0 {
		// No selector — return empty list rather than an error.
		writeJSON(w, map[string]any{"items": []any{}})
		return
	}

	// 3. Build a label selector string ("key1=val1,key2=val2").
	parts := make([]string, 0, len(matchLabels))
	for k, v := range matchLabels {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	selector := strings.Join(parts, ",")

	// 4. List pods in the same namespace with that selector.
	podGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	podList, err := h.dynamic.Resource(podGVR).Namespace(ns).List(r.Context(), metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		http.Error(w, err.Error(), statusFromK8sErr(err))
		return
	}

	// 5. Return full pod objects so the frontend can read metadata, status, etc.
	items := make([]map[string]any, 0, len(podList.Items))
	for _, pod := range podList.Items {
		items = append(items, pod.Object)
	}

	writeJSON(w, map[string]any{"items": items})
}

// --- Helpers -----------------------------------------------------------------

// gvrFromRequest extracts the GroupVersionResource from route parameters.
// "core" is mapped to the empty group for the K8s API.
func gvrFromRequest(r *http.Request) schema.GroupVersionResource {
	group := r.PathValue("group")
	if group == "core" {
		group = ""
	}
	return schema.GroupVersionResource{
		Group:    group,
		Version:  r.PathValue("version"),
		Resource: r.PathValue("kind"), // "kind" param is actually the plural resource name
	}
}

// summarize produces a flat map of key fields for the resource list table.
func summarize(obj unstructured.Unstructured) map[string]any {
	row := map[string]any{
		"name":      obj.GetName(),
		"namespace": obj.GetNamespace(),
		"kind":      obj.GetKind(),
		"uid":       string(obj.GetUID()),
		"labels":    obj.GetLabels(),
	}

	// Age
	ts := obj.GetCreationTimestamp()
	if !ts.IsZero() {
		row["createdAt"] = ts.Format(time.RFC3339)
		row["age"] = time.Since(ts.Time).Truncate(time.Second).String()
	}

	// Status — varies by kind; pull common patterns.
	if phase, ok := nestedString(obj, "status", "phase"); ok {
		row["status"] = phase
	}

	// Replicas (Deployment/StatefulSet/ReplicaSet)
	if desired, ok := nestedInt(obj, "spec", "replicas"); ok {
		row["desiredReplicas"] = desired
	}
	if ready, ok := nestedInt(obj, "status", "readyReplicas"); ok {
		row["readyReplicas"] = ready
	}
	if avail, ok := nestedInt(obj, "status", "availableReplicas"); ok {
		row["availableReplicas"] = avail
	}

	// Pod status
	if containerStatuses, ok, _ := unstructured.NestedSlice(obj.Object, "status", "containerStatuses"); ok {
		ready := 0
		total := len(containerStatuses)
		for _, cs := range containerStatuses {
			if csm, ok := cs.(map[string]any); ok {
				if r, ok := csm["ready"].(bool); ok && r {
					ready++
				}
			}
		}
		row["readyContainers"] = ready
		row["totalContainers"] = total
	}
	if restarts, ok := podRestarts(obj); ok {
		row["restarts"] = restarts
	}

	// Node (for pods)
	if nodeName, ok := nestedString(obj, "spec", "nodeName"); ok {
		row["nodeName"] = nodeName
	}

	// Service type
	if svcType, ok := nestedString(obj, "spec", "type"); ok && obj.GetKind() == "Service" {
		row["serviceType"] = svcType
	}

	// Cluster IP
	if clusterIP, ok := nestedString(obj, "spec", "clusterIP"); ok {
		row["clusterIP"] = clusterIP
	}

	// Node conditions
	if conditions, ok, _ := unstructured.NestedSlice(obj.Object, "status", "conditions"); ok {
		for _, c := range conditions {
			if cm, ok := c.(map[string]any); ok {
				if cm["type"] == "Ready" {
					row["ready"] = cm["status"]
				}
			}
		}
	}

	return row
}

func nestedString(obj unstructured.Unstructured, fields ...string) (string, bool) {
	val, ok, _ := unstructured.NestedString(obj.Object, fields...)
	return val, ok
}

func nestedInt(obj unstructured.Unstructured, fields ...string) (int64, bool) {
	val, ok, _ := unstructured.NestedInt64(obj.Object, fields...)
	return val, ok
}

func podRestarts(obj unstructured.Unstructured) (int64, bool) {
	css, ok, _ := unstructured.NestedSlice(obj.Object, "status", "containerStatuses")
	if !ok {
		return 0, false
	}
	var total int64
	for _, cs := range css {
		if csm, ok := cs.(map[string]any); ok {
			if rc, ok := csm["restartCount"].(int64); ok {
				total += rc
			} else if rc, ok := csm["restartCount"].(float64); ok {
				total += int64(rc)
			}
		}
	}
	return total, true
}

func statusFromK8sErr(err error) int {
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "not found"),
		strings.Contains(s, "could not find"):
		return http.StatusNotFound
	case strings.Contains(s, "forbidden"):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Error().Err(err).Msg("json encode error")
	}
}
