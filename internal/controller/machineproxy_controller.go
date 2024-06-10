/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	glaceonv1alpha1 "github.com/Twi/glaceon-operator/api/v1alpha1"
	"github.com/Twi/glaceon-operator/internal/flyctl"
)

const machineproxyFinalizer = "glaceon.friendshipcastle.zip/finalizer"

// Definitions to manage status conditions
const (
	// typeAvailableMachineProxy represents the status of the Deployment reconciliation
	typeAvailableMachineProxy = "Available"
	// typeDegradedMachineProxy represents the status used when the custom resource is deleted and the finalizer operations are must to occur.
	typeDegradedMachineProxy = "Degraded"
)

// MachineProxyReconciler reconciles a MachineProxy object
type MachineProxyReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// The following markers are used to generate the rules permissions (RBAC) on config/rbac using controller-gen
// when the command <make manifests> is executed.
// To know more about markers see: https://book.kubebuilder.io/reference/markers.html

//+kubebuilder:rbac:groups=glaceon.friendshipcastle.zip,resources=machineproxies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=glaceon.friendshipcastle.zip,resources=machineproxies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=glaceon.friendshipcastle.zip,resources=machineproxies/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// It is essential for the controller's reconciliation loop to be idempotent. By following the Operator
// pattern you will create Controllers which provide a reconcile function
// responsible for synchronizing resources until the desired state is reached on the cluster.
// Breaking this recommendation goes against the design principles of controller-runtime.
// and may lead to unforeseen consequences such as resources becoming stuck and requiring manual intervention.
// For further info:
// - About Operator Pattern: https://kubernetes.io/docs/concepts/extend-kubernetes/operator/
// - About Controllers: https://kubernetes.io/docs/concepts/architecture/controller/
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *MachineProxyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the MachineProxy instance
	// The purpose is check if the Custom Resource for the Kind MachineProxy
	// is applied on the cluster if not we return nil to stop the reconciliation
	machineproxy := &glaceonv1alpha1.MachineProxy{}
	err := r.Get(ctx, req.NamespacedName, machineproxy)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the custom resource is not found then, it usually means that it was deleted or not created
			// In this way, we will stop the reconciliation
			log.Info("machineproxy resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get machineproxy")
		return ctrl.Result{}, err
	}

	// Let's just set the status as Unknown when no status are available
	if machineproxy.Status.Conditions == nil || len(machineproxy.Status.Conditions) == 0 {
		meta.SetStatusCondition(&machineproxy.Status.Conditions, metav1.Condition{Type: typeAvailableMachineProxy, Status: metav1.ConditionUnknown, Reason: "Reconciling", Message: "Starting reconciliation"})
		if err = r.Status().Update(ctx, machineproxy); err != nil {
			log.Error(err, "Failed to update MachineProxy status")
			return ctrl.Result{}, err
		}

		// Let's re-fetch the machineproxy Custom Resource after update the status
		// so that we have the latest state of the resource on the cluster and we will avoid
		// raise the issue "the object has been modified, please apply
		// your changes to the latest version and try again" which would re-trigger the reconciliation
		// if we try to update it again in the following operations
		if err := r.Get(ctx, req.NamespacedName, machineproxy); err != nil {
			log.Error(err, "Failed to re-fetch machineproxy")
			return ctrl.Result{}, err
		}
	}

	// Let's add a finalizer. Then, we can define some operations which should
	// occurs before the custom resource to be deleted.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers
	if !controllerutil.ContainsFinalizer(machineproxy, machineproxyFinalizer) {
		log.Info("Adding Finalizer for MachineProxy")
		if ok := controllerutil.AddFinalizer(machineproxy, machineproxyFinalizer); !ok {
			log.Error(err, "Failed to add finalizer into the custom resource")
			return ctrl.Result{Requeue: true}, nil
		}

		if err = r.Update(ctx, machineproxy); err != nil {
			log.Error(err, "Failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Check if the MachineProxy instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	isMachineProxyMarkedToBeDeleted := machineproxy.GetDeletionTimestamp() != nil
	if isMachineProxyMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(machineproxy, machineproxyFinalizer) {
			log.Info("Performing Finalizer Operations for MachineProxy before delete CR")

			// Let's add here an status "Downgrade" to define that this resource begin its process to be terminated.
			meta.SetStatusCondition(&machineproxy.Status.Conditions, metav1.Condition{Type: typeDegradedMachineProxy,
				Status: metav1.ConditionUnknown, Reason: "Finalizing",
				Message: fmt.Sprintf("Performing finalizer operations for the custom resource: %s ", machineproxy.Name)})

			if err := r.Status().Update(ctx, machineproxy); err != nil {
				log.Error(err, "Failed to update MachineProxy status")
				return ctrl.Result{}, err
			}

			// Perform all operations required before remove the finalizer and allow
			// the Kubernetes API to remove the custom resource.
			r.doFinalizerOperationsForMachineProxy(ctx, machineproxy)

			// TODO(user): If you add operations to the doFinalizerOperationsForMachineProxy method
			// then you need to ensure that all worked fine before deleting and updating the Downgrade status
			// otherwise, you should requeue here.

			// Re-fetch the machineproxy Custom Resource before update the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raise the issue "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, machineproxy); err != nil {
				log.Error(err, "Failed to re-fetch machineproxy")
				return ctrl.Result{}, err
			}

			meta.SetStatusCondition(&machineproxy.Status.Conditions, metav1.Condition{Type: typeDegradedMachineProxy,
				Status: metav1.ConditionTrue, Reason: "Finalizing",
				Message: fmt.Sprintf("Finalizer operations for custom resource %s name were successfully accomplished", machineproxy.Name)})

			if err := r.Status().Update(ctx, machineproxy); err != nil {
				log.Error(err, "Failed to update MachineProxy status")
				return ctrl.Result{}, err
			}

			log.Info("Removing Finalizer for MachineProxy after successfully perform the operations")
			if ok := controllerutil.RemoveFinalizer(machineproxy, machineproxyFinalizer); !ok {
				log.Error(err, "Failed to remove finalizer for MachineProxy")
				return ctrl.Result{Requeue: true}, nil
			}

			if err := r.Update(ctx, machineproxy); err != nil {
				log.Error(err, "Failed to remove finalizer for MachineProxy")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	secretFound := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: machineproxy.Name, Namespace: machineproxy.Namespace}, secretFound); err != nil && apierrors.IsNotFound(err) {
		// define a new secret
		sec, err := r.secretForMachineProxy(ctx, machineproxy)
		if err != nil {
			log.Error(err, "Failed to define new Secret resource for MachineProxy")

			// The following implementation will update the status
			meta.SetStatusCondition(&machineproxy.Status.Conditions, metav1.Condition{Type: typeAvailableMachineProxy,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Secret (%s): (%s)", machineproxy.Name, err)})

			if err := r.Status().Update(ctx, machineproxy); err != nil {
				log.Error(err, "Failed to update MachineProxy status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		log.Info("Creating a new Secret", "Secret.Namespace", sec.Namespace, "Secret.Name", sec.Name)
		if err := r.Create(ctx, sec); err != nil {
			log.Error(err, "Failed to create new Secret", "Secret.Namespace", sec.Namespace, "Secret.Name", sec.Name)
			return ctrl.Result{}, err
		}

		secretFound = sec
	} else if err != nil {
		log.Error(err, "Failed to get Secret")
		// Let's return the error for the reconciliation be re-trigged again
		return ctrl.Result{}, err
	}

	serviceFound := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Name: machineproxy.Name, Namespace: machineproxy.Namespace}, serviceFound); err != nil && apierrors.IsNotFound(err) {
		// define a new service
		svc, err := r.serviceForMachineProxy(machineproxy)
		if err != nil {
			log.Error(err, "Failed to define new Service resource for MachineProxy")

			// The following implementation will update the status
			meta.SetStatusCondition(&machineproxy.Status.Conditions, metav1.Condition{Type: typeAvailableMachineProxy,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Service (%s): (%s)", machineproxy.Name, err)})

			if err := r.Status().Update(ctx, machineproxy); err != nil {
				log.Error(err, "Failed to update MachineProxy status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		log.Info("Creating a new Service", "Service.Namespace", svc.Namespace, "Service.Name", svc.Name)
		if err := r.Create(ctx, svc); err != nil {
			log.Error(err, "Failed to create new Service", "Service.Namespace", svc.Namespace, "Service.Name", svc.Name)
			return ctrl.Result{}, err
		}
	} else if err != nil {
		log.Error(err, "Failed to get Service")
		// Let's return the error for the reconciliation be re-trigged again
		return ctrl.Result{}, err
	}

	// Check if the deployment already exists, if not create a new one
	found := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: machineproxy.Name, Namespace: machineproxy.Namespace}, found)
	if err != nil && apierrors.IsNotFound(err) {
		// Define a new deployment
		dep, err := r.deploymentForMachineProxy(machineproxy, secretFound)
		if err != nil {
			log.Error(err, "Failed to define new Deployment resource for MachineProxy")

			// The following implementation will update the status
			meta.SetStatusCondition(&machineproxy.Status.Conditions, metav1.Condition{Type: typeAvailableMachineProxy,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Deployment (%s): (%s)", machineproxy.Name, err)})

			if err := r.Status().Update(ctx, machineproxy); err != nil {
				log.Error(err, "Failed to update MachineProxy status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		log.Info("Creating a new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
		if err = r.Create(ctx, dep); err != nil {
			log.Error(err, "Failed to create new Deployment",
				"Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
			return ctrl.Result{}, err
		}

		// Deployment created successfully
		// We will requeue the reconciliation so that we can ensure the state
		// and move forward for the next operations
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	} else if err != nil {
		log.Error(err, "Failed to get Deployment")
		// Let's return the error for the reconciliation be re-trigged again
		return ctrl.Result{}, err
	}

	// The following implementation will update the status
	meta.SetStatusCondition(&machineproxy.Status.Conditions, metav1.Condition{Type: typeAvailableMachineProxy,
		Status: metav1.ConditionTrue, Reason: "Reconciling",
		Message: fmt.Sprintf("Deployment for custom resource (%s) created successfully", machineproxy.Name)})

	if err := r.Status().Update(ctx, machineproxy); err != nil {
		log.Error(err, "Failed to update MachineProxy status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// finalizeMachineProxy will perform the required operations before delete the CR.
func (r *MachineProxyReconciler) doFinalizerOperationsForMachineProxy(ctx context.Context, cr *glaceonv1alpha1.MachineProxy) {
	log := log.FromContext(ctx)
	// TODO(user): Add the cleanup steps that the operator
	// needs to do before the CR can be deleted. Examples
	// of finalizers include performing backups and deleting
	// resources that are not owned by this CR, like a PVC.

	// Note: It is not recommended to use finalizers with the purpose of delete resources which are
	// created and managed in the reconciliation. These ones, such as the Deployment created on this reconcile,
	// are defined as depended of the custom resource. See that we use the method ctrl.SetControllerReference.
	// to set the ownerRef which means that the Deployment will be deleted by the Kubernetes API.
	// More info: https://kubernetes.io/docs/tasks/administer-cluster/use-cascading-deletion/

	secretFound := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, secretFound); err != nil && apierrors.IsNotFound(err) {
		log.Error(err, "Failed to find secret, manual intervention may be required")
		return
	}

	if err := flyctl.WireGuardRemove(ctx, cr.Spec.Org, secretFound.Annotations["glaceon.friendshipcastle.zip/wireguard-peer-name"]); err != nil {
		log.Error(err, "Failed to delete wireguard peer, manual intervention may be required")
	}

	// The following implementation will raise an event
	r.Recorder.Event(cr, "Warning", "Deleting",
		fmt.Sprintf("Custom Resource %s is being deleted from the namespace %s", cr.Name, cr.Namespace))
}

func (r *MachineProxyReconciler) secretForMachineProxy(ctx context.Context, machineproxy *glaceonv1alpha1.MachineProxy) (*corev1.Secret, error) {
	ls := labelsForMachineProxy(machineproxy.Name)
	org := machineproxy.Spec.Org
	region := machineproxy.Spec.Region

	randomPrefix := RandStringBytes(16)
	name := randomPrefix + "-" + machineproxy.Name

	config, err := flyctl.WireGuardCreate(ctx, org, region, randomPrefix+name)
	if err != nil {
		return nil, fmt.Errorf("can't create wireguard peer: %w", err)
	}

	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineproxy.Name,
			Namespace: machineproxy.Namespace,
			Labels:    ls,
			Annotations: map[string]string{
				"glaceon.friendshipcastle.zip/wireguard-peer-name": name,
			},
		},
		Immutable: &[]bool{true}[0],
		Data: map[string][]byte{
			"fly0.conf": []byte(config),
		},
	}

	// Set the ownerRef for the Deployment
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
	if err := ctrl.SetControllerReference(machineproxy, sec, r.Scheme); err != nil {
		return nil, err
	}

	return sec, nil
}

func (r *MachineProxyReconciler) serviceForMachineProxy(machineproxy *glaceonv1alpha1.MachineProxy) (*corev1.Service, error) {
	ls := labelsForMachineProxy(machineproxy.Name)
	port := machineproxy.Spec.Port

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineproxy.Name,
			Namespace: machineproxy.Namespace,
			Labels:    ls,
		},
		Spec: corev1.ServiceSpec{
			Selector: ls,
			Type:     corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       port,
					TargetPort: intstr.FromInt32(8080),
				},
			},
		},
	}

	// Set the ownerRef for the Deployment
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
	if err := ctrl.SetControllerReference(machineproxy, svc, r.Scheme); err != nil {
		return nil, err
	}

	return svc, nil
}

// deploymentForMachineProxy returns a MachineProxy Deployment object
func (r *MachineProxyReconciler) deploymentForMachineProxy(machineproxy *glaceonv1alpha1.MachineProxy, sec *corev1.Secret) (*appsv1.Deployment, error) {
	ls := labelsForMachineProxy(machineproxy.Name)
	var replicas int32 = 1

	// Get the Operand image
	image, err := imageForMachineProxy()
	if err != nil {
		return nil, err
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineproxy.Name,
			Namespace: machineproxy.Namespace,
			Labels:    ls,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "wireguard-secret",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: sec.Name,
								},
							},
						},
					},
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &[]bool{true}[0],
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
						FSGroup: &[]int64{1000}[0],
					},
					Containers: []corev1.Container{{
						Image:           image,
						Name:            "machineproxy",
						ImagePullPolicy: corev1.PullIfNotPresent,
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot:             &[]bool{true}[0],
							RunAsUser:                &[]int64{1000}[0],
							AllowPrivilegeEscalation: &[]bool{false}[0],
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{
									"ALL",
								},
							},
						},
						Env: []corev1.EnvVar{
							{
								Name:  "PROXY_TO",
								Value: machineproxy.Spec.Target,
							},
							{
								Name:  "LISTEN",
								Value: ":8080",
							},
							{
								Name:  "WIREGUARD_CONFIG_FNAME",
								Value: "/run/secrets/wireguard/fly0.conf",
							},
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "wireguard-secret",
								ReadOnly:  true,
								MountPath: "/run/secrets/wireguard",
							},
						},
						Command: []string{"/usr/bin/glaceon"},
					}},
				},
			},
		},
	}

	// Set the ownerRef for the Deployment
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
	if err := ctrl.SetControllerReference(machineproxy, dep, r.Scheme); err != nil {
		return nil, err
	}
	return dep, nil
}

// labelsForMachineProxy returns the labels for selecting the resources
// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func labelsForMachineProxy(name string) map[string]string {
	var imageTag string
	image, err := imageForMachineProxy()
	if err == nil {
		imageTag = strings.Split(image, ":")[1]
	}
	return map[string]string{"app.kubernetes.io/name": "MachineProxy",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/version":    imageTag,
		"app.kubernetes.io/part-of":    "glaceon-operator",
		"app.kubernetes.io/created-by": "controller-manager",
	}
}

// imageForMachineProxy gets the Operand image which is managed by this controller
// from the MACHINEPROXY_IMAGE environment variable defined in the config/manager/manager.yaml
func imageForMachineProxy() (string, error) {
	var imageEnvVar = "MACHINEPROXY_IMAGE"
	image, found := os.LookupEnv(imageEnvVar)
	if !found {
		return "", fmt.Errorf("Unable to find %s environment variable with the image", imageEnvVar)
	}
	return image, nil
}

// SetupWithManager sets up the controller with the Manager.
// Note that the Deployment will be also watched in order to ensure its
// desirable state on the cluster
func (r *MachineProxyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&glaceonv1alpha1.MachineProxy{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

const letterBytes = "abcdefghijklmnopqrstuvwxyz0123456789"

func RandStringBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}
