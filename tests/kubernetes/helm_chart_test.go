package kubernetes

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const chartDir = "../../deploy/helm/liteaig"

func readChartFile(t *testing.T, parts ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{chartDir}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func renderHelm(t *testing.T, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm is not installed")
	}
	cmd := exec.Command("helm", append([]string{"template", "liteaig", chartDir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, out)
	}
	return string(out)
}

func readValues(t *testing.T) map[string]any {
	t.Helper()
	var values map[string]any
	if err := yaml.Unmarshal([]byte(readChartFile(t, "values.yaml")), &values); err != nil {
		t.Fatal(err)
	}
	return values
}

func workload(t *testing.T, values map[string]any, name string) map[string]any {
	t.Helper()
	workloads := values["workloads"].(map[string]any)
	return workloads[name].(map[string]any)
}

func TestChartDefaultsEncodeGatewayHABaseline(t *testing.T) {
	values := readValues(t)
	gateway := workload(t, values, "gateway")
	if gateway["mode"] != "gateway" || gateway["replicas"].(int) < 3 {
		t.Fatalf("gateway defaults = %+v", gateway)
	}
	pdb := gateway["pdb"].(map[string]any)
	if pdb["minAvailable"].(int) < 2 || pdb["minAvailable"].(int) >= gateway["replicas"].(int) {
		t.Fatalf("gateway PDB = %+v replicas=%v", pdb, gateway["replicas"])
	}
	rollout := gateway["rollingUpdate"].(map[string]any)
	if rollout["maxUnavailable"].(int) >= gateway["replicas"].(int) || rollout["maxSurge"].(int) < 1 {
		t.Fatalf("rollout = %+v", rollout)
	}
	drain := values["drain"].(map[string]any)
	if drain["terminationGracePeriodSeconds"].(int) < drain["forceShutdownTimeoutSeconds"].(int) {
		t.Fatalf("drain = %+v", drain)
	}
}

func TestChartSchemaAndHelpersRejectInvalidHAValues(t *testing.T) {
	schema := readChartFile(t, "values.schema.json")
	for _, required := range []string{"ScheduleAnyway", "DoNotSchedule", "gateway", "control", "all", "minReplicas", "maxReplicas"} {
		if !strings.Contains(schema, required) {
			t.Fatalf("schema missing %q", required)
		}
	}
	helpers := readChartFile(t, "templates", "_helpers.tpl")
	for _, required := range []string{
		"pdb.minAvailable must be lower than replicas",
		"rollingUpdate.maxUnavailable must be lower than replicas",
		"terminationGracePeriodSeconds must cover forceShutdownTimeoutSeconds",
		"autoscaling.minReplicas must not exceed maxReplicas",
	} {
		if !strings.Contains(helpers, required) || !strings.Contains(helpers, "fail") {
			t.Fatalf("validation helper missing %q", required)
		}
	}
}

func TestWorkloadTemplatesWireProbesDrainPlacementAndPDB(t *testing.T) {
	deployment := readChartFile(t, "templates", "deployment.yaml")
	for _, required := range []string{
		"path: /healthz",
		"path: /readyz",
		"--drain-timeout=",
		"--stream-drain-timeout=",
		"--force-shutdown-timeout=",
		"topologySpreadConstraints",
		"$.Values.topology.key",
		"podAntiAffinity",
		"terminationGracePeriodSeconds",
		"maxUnavailable",
		"maxSurge",
	} {
		if !strings.Contains(deployment, required) {
			t.Fatalf("deployment template missing %q", required)
		}
	}
	pdb := readChartFile(t, "templates", "pdb.yaml")
	if !strings.Contains(pdb, "kind: PodDisruptionBudget") || !strings.Contains(pdb, "minAvailable") || !strings.Contains(pdb, "matchLabels") {
		t.Fatalf("PDB template incomplete: %s", pdb)
	}
	service := readChartFile(t, "templates", "service.yaml")
	if !strings.Contains(service, "kind: Service") || !strings.Contains(service, "targetPort: http") {
		t.Fatalf("service template incomplete: %s", service)
	}
}

func TestAutoscalingTemplateIsDrainAwareAndExtensible(t *testing.T) {
	hpa := readChartFile(t, "templates", "hpa.yaml")
	for _, required := range []string{
		"apiVersion: autoscaling/v2",
		"kind: HorizontalPodAutoscaler",
		"stabilizationWindowSeconds",
		"averageUtilization",
		"customMetrics",
	} {
		if !strings.Contains(hpa, required) {
			t.Fatalf("HPA template missing %q", required)
		}
	}
	values := readValues(t)
	gateway := workload(t, values, "gateway")
	autoscaling := gateway["autoscaling"].(map[string]any)
	if autoscaling["minReplicas"].(int) > autoscaling["maxReplicas"].(int) || autoscaling["scaleDownStabilizationSeconds"].(int) < 300 {
		t.Fatalf("autoscaling defaults = %+v", autoscaling)
	}
}

func TestRenderedInputsAreSecretMinimizing(t *testing.T) {
	for _, file := range []string{"values.yaml", "templates/deployment.yaml", "templates/NOTES.txt"} {
		source := strings.ToLower(readChartFile(t, strings.Split(file, "/")...))
		for _, forbidden := range []string{"api_key:", "password:", "client_secret", "provider_secret", "secret data"} {
			if strings.Contains(source, forbidden) {
				t.Fatalf("%s contains forbidden secret marker %q", file, forbidden)
			}
		}
	}
	deployment := readChartFile(t, "templates", "deployment.yaml")
	if !strings.Contains(deployment, "secretKeyRef") || !strings.Contains(deployment, "existingSecret.name") {
		t.Fatal("chart should support secret references without inline values")
	}
}

func TestScenarioGKubernetesPolicyAssertions(t *testing.T) {
	values := readValues(t)
	gateway := workload(t, values, "gateway")
	if !gateway["enabled"].(bool) {
		t.Fatal("gateway workload must be enabled by default")
	}
	for _, template := range []string{"deployment.yaml", "pdb.yaml"} {
		source := readChartFile(t, "templates", template)
		if !strings.Contains(source, "liteaig.selectorLabels") {
			t.Fatalf("%s does not isolate selectors to the LiteAIG workload", template)
		}
	}
	hpa := readChartFile(t, "templates", "hpa.yaml")
	if !strings.Contains(hpa, "scaleTargetRef") || !strings.Contains(hpa, "kind: Deployment") {
		t.Fatal("HPA must target the workload Deployment")
	}
	topology := values["topology"].(map[string]any)
	if topology["key"] != "topology.kubernetes.io/zone" || topology["maxSkew"].(int) != 1 {
		t.Fatalf("topology defaults = %+v", topology)
	}
}

func TestSplitPlaneExampleRendersStandardTierMultiReplica(t *testing.T) {
	rendered := renderHelm(t, "-f", filepath.Join(chartDir, "examples", "split-plane-values.yaml"))
	for _, required := range []string{
		"name: liteaig-liteaig-gateway",
		"name: liteaig-liteaig-control",
		"replicas: 3",
		"replicas: 2",
		"--mode=gateway",
		"--mode=control",
		"--db=$(LITEAIG_DATABASE_DSN)",
		"--coordinator=$(LITEAIG_COORDINATOR_URL)",
		"--control-url=http://liteaig-liteaig-control:8080",
		"--bundle-token=$(LITEAIG_BUNDLE_TOKEN)",
		"--bundle-public-key=$(LITEAIG_BUNDLE_PUBLIC_KEY)",
		"--bundle-signing-key=$(LITEAIG_BUNDLE_SIGNING_KEY)",
		"key: database_dsn",
		"key: coordinator_url",
		"key: liteaig_master_key",
	} {
		if !strings.Contains(rendered, required) {
			t.Fatalf("split-plane render missing %q\n%s", required, rendered)
		}
	}
	for _, forbidden := range []string{"kind: PersistentVolumeClaim", "--db=file:/data/liteaig.db"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("split-plane render included SQLite artifact %q", forbidden)
		}
	}
}
