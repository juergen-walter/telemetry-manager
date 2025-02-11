package operator

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	operatorv1alpha1 "github.com/kyma-project/telemetry-manager/apis/operator/v1alpha1"
	telemetryv1alpha1 "github.com/kyma-project/telemetry-manager/apis/telemetry/v1alpha1"
	"github.com/kyma-project/telemetry-manager/internal/conditions"
	"github.com/kyma-project/telemetry-manager/internal/testutils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Deploying a Telemetry", Ordered, func() {
	const (
		timeout            = time.Second * 10
		interval           = time.Millisecond * 250
		telemetryNamespace = "default"
	)

	Context("When no dependent resources exist", Ordered, func() {
		const telemetryName = "telemetry-1"

		BeforeAll(func() {
			telemetry := &operatorv1alpha1.Telemetry{
				ObjectMeta: metav1.ObjectMeta{
					Name:      telemetryName,
					Namespace: telemetryNamespace,
				},
			}

			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, telemetry)).Should(Succeed())
			})
			Expect(k8sClient.Create(ctx, telemetry)).Should(Succeed())
		})

		It("Should have Telemetry with ready state", func() {
			Eventually(func() (operatorv1alpha1.State, error) {
				lookupKey := types.NamespacedName{
					Name:      telemetryName,
					Namespace: telemetryNamespace,
				}
				var telemetry operatorv1alpha1.Telemetry
				err := k8sClient.Get(ctx, lookupKey, &telemetry)
				if err != nil {
					return "", err
				}

				return telemetry.Status.State, nil
			}, timeout, interval).Should(Equal(operatorv1alpha1.StateReady))
		})
	})

	Context("When a running TracePipeline exists", Ordered, func() {
		const telemetryName = "telemetry-2"

		BeforeAll(func() {
			telemetry := &operatorv1alpha1.Telemetry{
				ObjectMeta: metav1.ObjectMeta{
					Name:      telemetryName,
					Namespace: telemetryNamespace,
				},
			}
			runningTracePipeline := testutils.NewTracePipelineBuilder().Build()

			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, &runningTracePipeline)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, telemetry)).Should(Succeed())
			})
			Expect(k8sClient.Create(ctx, &runningTracePipeline)).Should(Succeed())
			runningTracePipeline.Status.SetCondition(telemetryv1alpha1.TracePipelineCondition{
				Reason: conditions.ReasonTraceGatewayDeploymentReady,
				Type:   telemetryv1alpha1.TracePipelineRunning,
			})
			Expect(k8sClient.Status().Update(ctx, &runningTracePipeline)).Should(Succeed())
			Expect(k8sClient.Create(ctx, telemetry)).Should(Succeed())
		})

		It("Should have Telemetry with ready state", func() {
			Eventually(func() (operatorv1alpha1.State, error) {
				lookupKey := types.NamespacedName{
					Name:      telemetryName,
					Namespace: telemetryNamespace,
				}
				var telemetry operatorv1alpha1.Telemetry
				err := k8sClient.Get(ctx, lookupKey, &telemetry)
				if err != nil {
					return "", err
				}

				return telemetry.Status.State, nil
			}, timeout, interval).Should(Equal(operatorv1alpha1.StateReady))
		})
	})

	Context("When a pending TracePipeline exists", Ordered, func() {
		const telemetryName = "telemetry-3"

		BeforeAll(func() {
			telemetry := &operatorv1alpha1.Telemetry{
				ObjectMeta: metav1.ObjectMeta{
					Name:      telemetryName,
					Namespace: telemetryNamespace,
				},
			}
			pendingTracePipeline := testutils.NewTracePipelineBuilder().Build()

			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, &pendingTracePipeline)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, telemetry)).Should(Succeed())
			})
			Expect(k8sClient.Create(ctx, &pendingTracePipeline)).Should(Succeed())
			pendingTracePipeline.Status.SetCondition(telemetryv1alpha1.TracePipelineCondition{
				Reason: conditions.ReasonTraceGatewayDeploymentNotReady,
				Type:   telemetryv1alpha1.TracePipelinePending,
			})
			Expect(k8sClient.Status().Update(ctx, &pendingTracePipeline)).Should(Succeed())
			Expect(k8sClient.Create(ctx, telemetry)).Should(Succeed())
		})

		It("Should have Telemetry with warning state", func() {
			Eventually(func() (operatorv1alpha1.State, error) {
				lookupKey := types.NamespacedName{
					Name:      telemetryName,
					Namespace: telemetryNamespace,
				}
				var telemetry operatorv1alpha1.Telemetry
				err := k8sClient.Get(ctx, lookupKey, &telemetry)
				if err != nil {
					return "", err
				}

				return telemetry.Status.State, nil
			}, timeout, interval).Should(Equal(operatorv1alpha1.StateWarning))
		})
	})

	Context("When a LogPipeline with Loki output exists", Ordered, func() {
		const (
			telemetryName = "telemetry-4"
			pipelineName  = "pipeline-with-loki-output"
		)

		BeforeAll(func() {
			telemetry := &operatorv1alpha1.Telemetry{
				ObjectMeta: metav1.ObjectMeta{
					Name:      telemetryName,
					Namespace: telemetryNamespace,
				},
			}
			logPipelineWithLokiOutput := &telemetryv1alpha1.LogPipeline{
				ObjectMeta: metav1.ObjectMeta{
					Name: pipelineName,
				},
				Spec: telemetryv1alpha1.LogPipelineSpec{
					Output: telemetryv1alpha1.Output{
						Loki: &telemetryv1alpha1.LokiOutput{
							URL: telemetryv1alpha1.ValueType{
								Value: "http://logging-loki:3100/loki/api/v1/push",
							},
						},
					}},
			}

			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, logPipelineWithLokiOutput)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, telemetry)).Should(Succeed())
			})
			Expect(k8sClient.Create(ctx, logPipelineWithLokiOutput)).Should(Succeed())
			logPipelineWithLokiOutput.Status.SetCondition(telemetryv1alpha1.LogPipelineCondition{
				Reason: conditions.ReasonUnsupportedLokiOutput,
				Type:   telemetryv1alpha1.LogPipelinePending,
			})
			Expect(k8sClient.Status().Update(ctx, logPipelineWithLokiOutput)).Should(Succeed())
			Expect(k8sClient.Create(ctx, telemetry)).Should(Succeed())
		})

		It("Should have Telemetry with warning state", func() {
			Eventually(func(g Gomega) {
				lookupKey := types.NamespacedName{
					Name:      telemetryName,
					Namespace: telemetryNamespace,
				}
				var telemetry operatorv1alpha1.Telemetry
				g.Expect(k8sClient.Get(ctx, lookupKey, &telemetry)).Should(Succeed())
				g.Expect(telemetry.Status.State).Should(Equal(operatorv1alpha1.StateWarning))
			}, timeout, interval).Should(Succeed())
		})
	})
})
