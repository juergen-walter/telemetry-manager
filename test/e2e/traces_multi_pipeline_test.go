//go:build e2e

package e2e

import (
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	telemetryv1alpha1 "github.com/kyma-project/telemetry-manager/apis/telemetry/v1alpha1"
	"github.com/kyma-project/telemetry-manager/internal/otelcollector/ports"
	kitk8s "github.com/kyma-project/telemetry-manager/test/testkit/k8s"
	kitkyma "github.com/kyma-project/telemetry-manager/test/testkit/kyma"
	kittrace "github.com/kyma-project/telemetry-manager/test/testkit/kyma/telemetry/trace"
	"github.com/kyma-project/telemetry-manager/test/testkit/mocks/backend"
	"github.com/kyma-project/telemetry-manager/test/testkit/mocks/urlprovider"
	kittraces "github.com/kyma-project/telemetry-manager/test/testkit/otlp/traces"
	"github.com/kyma-project/telemetry-manager/test/testkit/verifiers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Traces Multi-Pipeline", Label("tracing"), func() {
	Context("When multiple tracepipelines exist", Ordered, func() {
		const (
			mockNs           = "traces-multi-pipeline"
			mockBackendName1 = "trace-receiver-1"
			mockBackendName2 = "trace-receiver-2"
		)
		var (
			pipelines = kitkyma.NewPipelineList()
			urls      = urlprovider.New()
		)

		makeResources := func() []client.Object {
			var objs []client.Object
			objs = append(objs, kitk8s.NewNamespace(mockNs).K8sObject())

			for _, backendName := range []string{mockBackendName1, mockBackendName2} {
				mockBackend := backend.New(backendName, mockNs, backend.SignalTypeTraces)
				objs = append(objs, mockBackend.K8sObjects()...)
				urls.SetMockBackendExport(mockBackend.Name(), mockBackend.TelemetryExportURL(proxyClient))

				pipeline := kittrace.NewPipeline(fmt.Sprintf("%s-%s", mockBackend.Name(), "pipeline")).WithOutputEndpointFromSecret(mockBackend.HostSecretRef())
				pipelines.Append(pipeline.Name())
				objs = append(objs, pipeline.K8sObject())
			}

			urls.SetOTLPPush(proxyClient.ProxyURLForService(
				kitkyma.SystemNamespaceName, "telemetry-otlp-traces", "v1/traces/", ports.OTLPHTTP),
			)

			return objs
		}

		BeforeAll(func() {
			k8sObjects := makeResources()

			DeferCleanup(func() {
				Expect(kitk8s.DeleteObjects(ctx, k8sClient, k8sObjects...)).Should(Succeed())
			})
			Expect(kitk8s.CreateObjects(ctx, k8sClient, k8sObjects...)).Should(Succeed())
		})

		It("Should have running pipelines", func() {
			verifiers.TracePipelineShouldBeRunning(ctx, k8sClient, pipelines.First())
			verifiers.TracePipelineShouldBeRunning(ctx, k8sClient, pipelines.Second())
		})
		It("Should have a trace backend running", func() {
			verifiers.DeploymentShouldBeReady(ctx, k8sClient, types.NamespacedName{Name: mockBackendName1, Namespace: mockNs})
			verifiers.DeploymentShouldBeReady(ctx, k8sClient, types.NamespacedName{Name: mockBackendName2, Namespace: mockNs})
		})
		It("Should verify end-to-end trace delivery", func() {
			traceID, spanIDs, attrs := kittraces.MakeAndSendTraces(proxyClient, urls.OTLPPush())
			verifiers.TracesShouldBeDelivered(proxyClient, urls.MockBackendExport(mockBackendName1), traceID, spanIDs, attrs)
			verifiers.TracesShouldBeDelivered(proxyClient, urls.MockBackendExport(mockBackendName2), traceID, spanIDs, attrs)
		})
	})

	Context("When reaching the pipeline limit", Ordered, func() {
		const maxNumberOfTracePipelines = 3
		var (
			pipelines            = kitkyma.NewPipelineList()
			pipelineCreatedFirst *telemetryv1alpha1.TracePipeline
			pipelineCreatedLater *telemetryv1alpha1.TracePipeline
		)

		makeResources := func() []client.Object {
			var objs []client.Object
			for i := 0; i < maxNumberOfTracePipelines; i++ {
				pipeline := kittrace.NewPipeline(fmt.Sprintf("pipeline-%d", i))
				pipelines.Append(pipeline.Name())
				objs = append(objs, pipeline.K8sObject())

				if i == 0 {
					pipelineCreatedFirst = pipeline.K8sObject()
				}
			}

			return objs
		}

		BeforeAll(func() {
			k8sObjects := makeResources()
			DeferCleanup(func() {
				k8sObjectsToDelete := slices.DeleteFunc(k8sObjects, func(obj client.Object) bool {
					return obj.GetName() == pipelineCreatedFirst.GetName() //first pipeline is deleted separately in one of the specs
				})
				k8sObjectsToDelete = append(k8sObjectsToDelete, pipelineCreatedLater)
				Expect(kitk8s.DeleteObjects(ctx, k8sClient, k8sObjectsToDelete...)).Should(Succeed())
			})
			Expect(kitk8s.CreateObjects(ctx, k8sClient, k8sObjects...)).Should(Succeed())
		})

		It("Should have only running pipelines", func() {
			for _, pipeline := range pipelines.All() {
				verifiers.TracePipelineShouldBeRunning(ctx, k8sClient, pipeline)
				verifiers.TraceCollectorConfigShouldContainPipeline(ctx, k8sClient, pipeline)
			}
		})

		It("Should have a pending pipeline", func() {
			By("Creating an additional pipeline", func() {
				pipeline := kittrace.NewPipeline("exceeding-pipeline")
				pipelineCreatedLater = pipeline.K8sObject()
				pipelines.Append(pipeline.Name())

				Expect(kitk8s.CreateObjects(ctx, k8sClient, pipeline.K8sObject())).Should(Succeed())
				verifiers.TracePipelineShouldNotBeRunning(ctx, k8sClient, pipeline.Name())
				verifiers.TraceCollectorConfigShouldNotContainPipeline(ctx, k8sClient, pipeline.Name())
			})
		})

		It("Should have only running pipelines", func() {
			By("Deleting a pipeline", func() {
				Expect(kitk8s.DeleteObjects(ctx, k8sClient, pipelineCreatedFirst)).Should(Succeed())

				for _, pipeline := range pipelines.All()[1:] {
					verifiers.TracePipelineShouldBeRunning(ctx, k8sClient, pipeline)
				}
			})
		})
	})

	Context("When a broken tracepipeline exists", Ordered, func() {
		const (
			mockBackendName = "traces-receiver"
			mockNs          = "traces-broken-pipeline"
		)
		var (
			urls                = urlprovider.New()
			healthyPipelineName string
			brokenPipelineName  string
		)

		makeResources := func() []client.Object {
			var objs []client.Object
			objs = append(objs, kitk8s.NewNamespace(mockNs).K8sObject())

			mockBackend := backend.New(mockBackendName, mockNs, backend.SignalTypeTraces)
			objs = append(objs, mockBackend.K8sObjects()...)
			urls.SetMockBackendExport(mockBackend.Name(), mockBackend.TelemetryExportURL(proxyClient))

			healthyPipeline := kittrace.NewPipeline("healthy").WithOutputEndpointFromSecret(mockBackend.HostSecretRef())
			healthyPipelineName = healthyPipeline.Name()
			objs = append(objs, healthyPipeline.K8sObject())

			unreachableHostSecret := kitk8s.NewOpaqueSecret("metric-rcv-hostname-broken", kitkyma.DefaultNamespaceName,
				kitk8s.WithStringData("metric-host", "http://unreachable:4317"))
			brokenPipeline := kittrace.NewPipeline("broken").WithOutputEndpointFromSecret(unreachableHostSecret.SecretKeyRef("metric-host"))
			brokenPipelineName = brokenPipeline.Name()
			objs = append(objs, brokenPipeline.K8sObject(), unreachableHostSecret.K8sObject())

			urls.SetOTLPPush(proxyClient.ProxyURLForService(
				kitkyma.SystemNamespaceName, "telemetry-otlp-traces", "v1/traces/", ports.OTLPHTTP),
			)

			return objs
		}

		BeforeAll(func() {
			k8sObjects := makeResources()

			DeferCleanup(func() {
				Expect(kitk8s.DeleteObjects(ctx, k8sClient, k8sObjects...)).Should(Succeed())
			})
			Expect(kitk8s.CreateObjects(ctx, k8sClient, k8sObjects...)).Should(Succeed())
		})

		It("Should have running pipelines", func() {
			verifiers.TracePipelineShouldBeRunning(ctx, k8sClient, healthyPipelineName)
			verifiers.TracePipelineShouldBeRunning(ctx, k8sClient, brokenPipelineName)
		})

		It("Should have a running trace gateway deployment", func() {
			verifiers.DeploymentShouldBeReady(ctx, k8sClient, kitkyma.TraceGatewayName)
		})

		It("Should have a trace backend running", func() {
			verifiers.DeploymentShouldBeReady(ctx, k8sClient, types.NamespacedName{Name: mockBackendName, Namespace: mockNs})
		})

		It("Should verify end-to-end trace delivery for the remaining pipeline", func() {
			traceID, spanIDs, attrs := kittraces.MakeAndSendTraces(proxyClient, urls.OTLPPush())
			verifiers.TracesShouldBeDelivered(proxyClient, urls.MockBackendExport(mockBackendName), traceID, spanIDs, attrs)
		})
	})
})
