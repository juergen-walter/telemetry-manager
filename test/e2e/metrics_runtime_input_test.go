//go:build e2e

package e2e

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kyma-project/telemetry-manager/internal/otelcollector/ports"
	kitk8s "github.com/kyma-project/telemetry-manager/test/testkit/k8s"
	kitkyma "github.com/kyma-project/telemetry-manager/test/testkit/kyma"
	kitmetric "github.com/kyma-project/telemetry-manager/test/testkit/kyma/telemetry/metric"
	. "github.com/kyma-project/telemetry-manager/test/testkit/matchers/metric"
	"github.com/kyma-project/telemetry-manager/test/testkit/mocks/backend"
	"github.com/kyma-project/telemetry-manager/test/testkit/periodic"
	"github.com/kyma-project/telemetry-manager/test/testkit/verifiers"
)

var (
	kubeletMetricNames = []string{
		"container.cpu.time",
		"container.cpu.utilization",
		"container.filesystem.available",
		"container.filesystem.capacity",
		"container.filesystem.usage",
		"container.memory.available",
		"container.memory.major_page_faults",
		"container.memory.page_faults",
		"container.memory.rss",
		"container.memory.usage",
		"container.memory.working_set",
		"k8s.pod.cpu.time",
		"k8s.pod.cpu.utilization",
		"k8s.pod.filesystem.available",
		"k8s.pod.filesystem.capacity",
		"k8s.pod.filesystem.usage",
		"k8s.pod.memory.available",
		"k8s.pod.memory.major_page_faults",
		"k8s.pod.memory.page_faults",
		"k8s.pod.memory.rss",
		"k8s.pod.memory.usage",
		"k8s.pod.memory.working_set",
		"k8s.pod.network.errors",
		"k8s.pod.network.io"}
	kubeletMetricResourceAttributes = []string{
		"k8s.cluster.name",
		"k8s.container.name",
		"k8s.namespace.name",
		"k8s.node.name",
		"k8s.pod.name",
		"k8s.pod.uid"}
)

var _ = Describe("Metrics Runtime Input", Label("metrics"), func() {
	const (
		mockNs          = "metric-runtime-input-mocks"
		mockBackendName = "metric-agent-receiver"
	)

	var (
		pipelineName       string
		telemetryExportURL string
	)

	makeResources := func() []client.Object {
		var objs []client.Object
		objs = append(objs, kitk8s.NewNamespace(mockNs).K8sObject())

		mockBackend := backend.New(mockBackendName, mockNs, backend.SignalTypeMetrics)
		objs = append(objs, mockBackend.K8sObjects()...)
		telemetryExportURL = mockBackend.TelemetryExportURL(proxyClient)

		metricPipeline := kitmetric.NewPipeline("pipeline-with-runtime-input-enabled").
			WithOutputEndpointFromSecret(mockBackend.HostSecretRef()).
			RuntimeInput(true)
		pipelineName = metricPipeline.Name()
		objs = append(objs, metricPipeline.K8sObject())

		return objs
	}

	Context("When a metricpipeline with runtime input exists", Ordered, func() {
		BeforeAll(func() {
			k8sObjects := makeResources()

			DeferCleanup(func() {
				Expect(kitk8s.DeleteObjects(ctx, k8sClient, k8sObjects...)).Should(Succeed())
			})

			Expect(kitk8s.CreateObjects(ctx, k8sClient, k8sObjects...)).Should(Succeed())
		})

		It("Should have a running metric gateway deployment", func() {
			verifiers.DeploymentShouldBeReady(ctx, k8sClient, kitkyma.MetricGatewayName)
		})

		It("Should have a metrics backend running", func() {
			verifiers.DeploymentShouldBeReady(ctx, k8sClient, types.NamespacedName{Name: mockBackendName, Namespace: mockNs})
		})

		It("Should have a running metric agent daemonset", func() {
			verifiers.DaemonSetShouldBeReady(ctx, k8sClient, kitkyma.MetricAgentName)
		})

		It("Should have a running pipeline", func() {
			verifiers.MetricPipelineShouldBeRunning(ctx, k8sClient, pipelineName)
		})

		It("Should verify kubelet metric names", func() {
			Eventually(func(g Gomega) {
				resp, err := proxyClient.Get(telemetryExportURL)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(resp).To(HaveHTTPStatus(http.StatusOK))
				g.Expect(resp).To(HaveHTTPBody(
					ContainMd(ContainMetric(WithName(BeElementOf(kubeletMetricNames)))),
				))
			}, periodic.TelemetryEventuallyTimeout, periodic.TelemetryInterval).Should(Succeed())
		})

		It("Should verify kubelet metric resource attributes", func() {
			Eventually(func(g Gomega) {
				resp, err := proxyClient.Get(telemetryExportURL)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(resp).To(HaveHTTPStatus(http.StatusOK))
				g.Expect(resp).To(HaveHTTPBody(
					ConsistOfMds(ContainResourceAttrs(HaveKey(BeElementOf(kubeletMetricResourceAttributes)))),
				))
			}, periodic.TelemetryEventuallyTimeout, periodic.TelemetryInterval).Should(Succeed())
		})

		It("Should be able to get metric agent metrics endpoint", Label(operationalTest), func() {
			agentMetricsURL := proxyClient.ProxyURLForService(kitkyma.MetricAgentMetrics.Namespace, kitkyma.MetricAgentMetrics.Name, "metrics", ports.Metrics)
			verifiers.ShouldExposeCollectorMetrics(proxyClient, agentMetricsURL)
		})

		It("Should have a working network policy", Label(operationalTest), func() {
			var networkPolicy networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, kitkyma.MetricAgentNetworkPolicy, &networkPolicy)).To(Succeed())

			Eventually(func(g Gomega) {
				var podList corev1.PodList
				g.Expect(k8sClient.List(ctx, &podList, client.InNamespace(kitkyma.SystemNamespaceName), client.MatchingLabels{"app.kubernetes.io/name": kitkyma.MetricAgentBaseName})).To(Succeed())
				g.Expect(podList.Items).NotTo(BeEmpty())

				metricAgentPodName := podList.Items[0].Name
				pprofEndpoint := proxyClient.ProxyURLForPod(kitkyma.SystemNamespaceName, metricAgentPodName, "debug/pprof/", ports.Pprof)

				resp, err := proxyClient.Get(pprofEndpoint)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(resp).To(HaveHTTPStatus(http.StatusServiceUnavailable))
			}, periodic.EventuallyTimeout, periodic.DefaultInterval).Should(Succeed())
		})
	})
})
