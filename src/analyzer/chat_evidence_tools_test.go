package main

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func defaultChatEngine(cs kubernetes.Interface) *ChatEngine {
	return &ChatEngine{clientset: cs}
}

func TestDescribePod_SurfacesCreateContainerConfigError(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "postfix-abc", Namespace: "utilities"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "postfix"}}},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "postfix",
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
					Reason:  "CreateContainerConfigError",
					Message: "Error: couldn't find key password in Secret utilities/brevo-smtp-creds",
				}},
			}},
		},
	}
	e := defaultChatEngine(fake.NewSimpleClientset(&pod))
	res := e.toolDescribePod(context.Background(), "utilities", "postfix-abc")
	if !strings.Contains(res.Text, "CreateContainerConfigError") {
		t.Fatalf("expected waiting reason in output:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "brevo-smtp-creds") {
		t.Fatalf("expected missing-secret-key message in output:\n%s", res.Text)
	}
}

func TestDescribePod_SurfacesCrashLoopExitCode(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "glpi-xyz", Namespace: "tooling"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "glpi"}}},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:         "glpi",
				RestartCount: 141,
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					Reason: "Error", ExitCode: 1, FinishedAt: metav1.NewTime(time.Now()),
				}},
				LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					Reason: "Error", ExitCode: 1,
				}},
			}},
		},
	}
	e := defaultChatEngine(fake.NewSimpleClientset(&pod))
	res := e.toolDescribePod(context.Background(), "tooling", "glpi-xyz")
	if !strings.Contains(res.Text, "exit=1") && !strings.Contains(res.Text, "exit code 1") {
		t.Fatalf("expected last-terminated exit code in output:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "141") {
		t.Fatalf("expected restart count in output:\n%s", res.Text)
	}
}

func TestDescribePod_SurfacesPodEvents(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "sonar-abc", Namespace: "utilities"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "sonarqube"}}},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "sonarqube",
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
					Reason:  "CreateContainerConfigError",
					Message: "Error: couldn't find key POSTGRES_USER in Secret utilities/sonarqube-db-secret",
				}},
			}},
		},
	}
	ev := corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "sonar-evt", Namespace: "utilities"},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: "utilities", Name: "sonar-abc"},
		Type:           "Warning", Reason: "Failed",
		Message: "Error: couldn't find key POSTGRES_USER in Secret utilities/sonarqube-db-secret",
	}
	e := defaultChatEngine(fake.NewSimpleClientset(&pod, &ev))
	res := e.toolDescribePod(context.Background(), "utilities", "sonar-abc")
	if !strings.Contains(res.Text, "Warning") || !strings.Contains(res.Text, "sonarqube-db-secret") {
		t.Fatalf("expected pod event (warning + secret key) in output:\n%s", res.Text)
	}
}

func TestGetPods_IncludesWaitingReason(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "jenkins-c-abc", Namespace: "utilities"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "jenkins"}}},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "jenkins",
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
					Reason:  "CreateContainerConfigError",
					Message: "Error: couldn't find key password in Secret utilities/brevo-smtp-creds",
				}},
			}},
		},
	}
	e := defaultChatEngine(fake.NewSimpleClientset(&pod))
	res := e.toolGetPods(context.Background(), "utilities")
	if !strings.Contains(res.Text, "CreateContainerConfigError") {
		t.Fatalf("expected waiting reason in get_pods output:\n%s", res.Text)
	}
}

func TestPodLogsFormatting_CurrentAndPrevious(t *testing.T) {
	format := func(previous bool) string {
		res := formatPodLogs("tooling", "bookstack-ab", "", previous, []byte("SQLSTATE[HY000] [2002] No such file or directory\nWaiting for DB to be available\n"))
		return res.Text
	}
	if !strings.Contains(format(false), "SQLSTATE") {
		t.Fatalf("current logs should include content:\n%s", format(false))
	}
	p := format(true)
	if !strings.Contains(p, "previous") || !strings.Contains(p, "SQLSTATE") {
		t.Fatalf("previous logs should be labelled and contain content:\n%s", p)
	}
}

func TestRolloutStatus_SurfacesProgressDeadlineExceeded(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bookstack",
			Namespace: "tooling",
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision": "2",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "bookstack"}},
		},
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{{
				Type:    appsv1.DeploymentProgressing,
				Status:  corev1.ConditionFalse,
				Reason:  "ProgressDeadlineExceeded",
				Message: `ReplicaSet "bookstack-6d6d7bc8c6" has timed out progressing.`,
			}, {
				Type:   appsv1.DeploymentAvailable,
				Status: corev1.ConditionFalse,
			}},
		},
	}
	rsOld := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bookstack-78986f67b5",
			Namespace: "tooling",
			Labels:    map[string]string{"app": "bookstack"},
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision": "1",
			},
		},
		Spec: appsv1.ReplicaSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "bookstack"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "bookstack"}},
			},
		},
		Status: appsv1.ReplicaSetStatus{Replicas: 1, ReadyReplicas: 0},
	}
	rsNew := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bookstack-6d6d7bc8c6",
			Namespace: "tooling",
			Labels:    map[string]string{"app": "bookstack"},
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision": "2",
			},
		},
		Spec: appsv1.ReplicaSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "bookstack"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "bookstack"}},
			},
		},
		Status: appsv1.ReplicaSetStatus{Replicas: 1, ReadyReplicas: 0},
	}
	e := defaultChatEngine(fake.NewSimpleClientset(deploy, rsOld, rsNew))
	res := e.toolRolloutStatus(context.Background(), "tooling", "bookstack")
	if !strings.Contains(res.Text, "ProgressDeadlineExceeded") {
		t.Fatalf("expected progressing condition in output:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "bookstack-78986f67b5") || !strings.Contains(res.Text, "bookstack-6d6d7bc8c6") {
		t.Fatalf("expected both replica sets in output:\n%s", res.Text)
	}
}

func TestJobStatus_SurfacesBackoffLimitExceeded(t *testing.T) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mariadb-bootstrap-app-databases",
			Namespace: "utilities",
			Labels:    map[string]string{"job-name": "mariadb-bootstrap-app-databases"},
		},
		Status: batchv1.JobStatus{
			Failed: 6,
			Conditions: []batchv1.JobCondition{{
				Type:    batchv1.JobFailed,
				Status:  corev1.ConditionTrue,
				Reason:  "BackoffLimitExceeded",
				Message: "Job has reached the specified backoff limit",
			}},
		},
	}
	e := defaultChatEngine(fake.NewSimpleClientset(job))
	res := e.toolJobStatus(context.Background(), "utilities", "mariadb-bootstrap-app-databases")
	if !strings.Contains(res.Text, "BackoffLimitExceeded") {
		t.Fatalf("expected job condition reason in output:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "6") {
		t.Fatalf("expected failed count in output:\n%s", res.Text)
	}
}

func TestSecretKeys_ListsKeysNotValues(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "bookstack-db-credentials", Namespace: "tooling"},
		Data: map[string][]byte{
			"DB_USERNAME": []byte("bookstack"),
			"DB_PASSWORD": []byte("bookstack-password-2026"),
		},
	}
	e := defaultChatEngine(fake.NewSimpleClientset(secret))
	res := e.toolSecretKeys(context.Background(), "tooling", "bookstack-db-credentials")
	if !strings.Contains(res.Text, "DB_USERNAME") || !strings.Contains(res.Text, "DB_PASSWORD") {
		t.Fatalf("expected data keys in output:\n%s", res.Text)
	}
	if strings.Contains(res.Text, "bookstack-password-2026") {
		t.Fatalf("secret_keys must never leak values:\n%s", res.Text)
	}
}

func TestSecretKeys_ReportsEmpty(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "brevo-smtp-creds", Namespace: "utilities"},
		Data:       map[string][]byte{},
	}
	e := defaultChatEngine(fake.NewSimpleClientset(secret))
	res := e.toolSecretKeys(context.Background(), "utilities", "brevo-smtp-creds")
	if !strings.Contains(res.Text, "NO data keys") || !strings.Contains(res.Text, "empty") {
		t.Fatalf("expected empty-secret signal in output:\n%s", res.Text)
	}
}

func TestChatToolCatalog_AdvertisesEvidenceTools(t *testing.T) {
	want := []string{"describe_pod", "pod_logs", "rollout_status", "job_status", "secret_keys"}
	for _, name := range want {
		found := false
		for _, spec := range chatToolSpecs() {
			if spec.Name == name {
				found = true
			}
		}
		if !found {
			t.Errorf("tool %q missing from chatToolSpecs()", name)
		}
	}
}

func TestChatSystemPrompt_EvidenceFirstRules(t *testing.T) {
	for _, want := range []string{"CreateContainerConfigError", "ImagePullBackOff", "CrashLoopBackOff", "logs --previous"} {
		if !strings.Contains(chatSystemPrompt, want) {
			t.Errorf("chatSystemPrompt missing evidence rule %q", want)
		}
	}
}
