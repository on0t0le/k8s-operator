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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/api/core/v1"
	// "k8s.io/apimachinery/pkg/util/intstr"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	apertureiov1alpha1 "github.com/on0t0le/k8s-operator/api/v1alpha1"
)

const finalizer = "aperture.io/application_controller_finalizer"

// ApplicationReconciler reconciles a Application object
type ApplicationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=aperture.io.aperture.io,resources=applications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=aperture.io.aperture.io,resources=applications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=aperture.io.aperture.io,resources=applications/finalizers,verbs=update

// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *ApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	var app apertureiov1alpha1.Application
	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		if apierrors.IsNotFound(err) {
			// we'll ignore not-found errors, since they can't be fixed by an immediate
			// requeue (we'll need to wait for a new notification), and we can get them
			// on deleted requests.
			return ctrl.Result{}, nil
		}
		l.Error(err, "unable to fetch Application")
		return ctrl.Result{}, err
	}
	if !controllerutil.ContainsFinalizer(&app, finalizer) {
		l.Info("Adding Finalizer")
		controllerutil.AddFinalizer(&app, finalizer)
		return ctrl.Result{}, r.Update(ctx, &app)
	}

	if !app.DeletionTimestamp.IsZero() {
		l.Info("Application is being deleted")
		return r.reconcileDelete(ctx, &app)
	}
	l.Info("Application is being created")
	return r.reconcileCreate(ctx, &app)
}

func (r *ApplicationReconciler) reconcileCreate(ctx context.Context, app *apertureiov1alpha1.Application) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	l.Info("Creating deployment")
	err := r.createOrUpdateDeployment(ctx, app)
	if err != nil {
		return ctrl.Result{}, err
	}
	// l.Info("Creating service")
	// err = r.createService(ctx, app)
	// if err != nil {
	// 	return ctrl.Result{}, err
	// }
	return ctrl.Result{RequeueAfter: time.Minute * 1}, nil
}

func (r *ApplicationReconciler) createOrUpdateDeployment(ctx context.Context, app *apertureiov1alpha1.Application) error {
	var depl appsv1.Deployment
	deplName := types.NamespacedName{Name: app.ObjectMeta.Name , Namespace: app.ObjectMeta.Namespace}
	if err := r.Get(ctx, deplName, &depl); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("unable to fetch Deployment: %v", err)
		}
		if apierrors.IsNotFound(err) {
			depl = appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:        app.ObjectMeta.Name,
					Namespace:   app.ObjectMeta.Namespace,
					Labels:      map[string]string{"label": app.ObjectMeta.Name, "app": app.ObjectMeta.Name},
					// Annotations: map[string]string{"imageregistry": "https://hub.docker.com/"},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: app.APIVersion,
							Kind:       app.Kind,
							Name:       app.Name,
							UID:        app.UID,
						},
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &app.Spec.Replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"label": app.ObjectMeta.Name},
					},
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"label": app.ObjectMeta.Name, "app": app.ObjectMeta.Name},
						},
						Spec: v1.PodSpec{
							// https://pkg.go.dev/k8s.io/api/core/v1#PodSpec.ImagePullSecrets
							// ImagePullSecrets []LocalObjectReference
							Containers: []v1.Container{
								{
									Name:  app.ObjectMeta.Name,
									Image: app.Spec.Image,
									Ports: []v1.ContainerPort{
										{
											ContainerPort: app.Spec.Port,
										},
									},
								},
							},
						},
					},
				},
			}
			err = r.Create(ctx, &depl)
			if err != nil {
				return fmt.Errorf("unable to create Deployment: %v", err)
			}
			return nil
		}
	}
	depl.Spec.Replicas = &app.Spec.Replicas
	depl.Spec.Template.Spec.Containers[0].Image = app.Spec.Image
	depl.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort = app.Spec.Port
	err := r.Update(ctx, &depl)
	if err != nil {
		return fmt.Errorf("unable to update Deployment: %v", err)
	}
	return nil
}

// func (r *ApplicationReconciler) createService(ctx context.Context, app *apertureiov1alpha1.Application) error {
// 	srv := v1.Service{
// 		ObjectMeta: metav1.ObjectMeta{
// 			Name:      app.ObjectMeta.Name + "-service",
// 			Namespace: app.ObjectMeta.Name,
// 			Labels:    map[string]string{"app": app.ObjectMeta.Name},
// 			OwnerReferences: []metav1.OwnerReference{
// 				{
// 					APIVersion: app.APIVersion,
// 					Kind:       app.Kind,
// 					Name:       app.Name,
// 					UID:        app.UID,
// 				},
// 			},
// 		},
// 		Spec: v1.ServiceSpec{
// 			Type:                  v1.ServiceTypeNodePort,
// 			ExternalTrafficPolicy: v1.ServiceExternalTrafficPolicyTypeLocal,
// 			Selector:              map[string]string{"app": app.ObjectMeta.Name},
// 			Ports: []v1.ServicePort{
// 				{
// 					Name:       "http",
// 					Port:       app.Spec.Port,
// 					Protocol:   v1.ProtocolTCP,
// 					TargetPort: intstr.FromInt(int(app.Spec.Port)),
// 				},
// 			},
// 		},
// 		Status: v1.ServiceStatus{},
// 	}
// 	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, &srv, func() error {
// 		return nil
// 	})
// 	if err != nil {
// 		return fmt.Errorf("unable to create Service: %v", err)
// 	}
// 	return nil
// }

func (r *ApplicationReconciler) reconcileDelete(ctx context.Context, app *apertureiov1alpha1.Application) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	l.Info("removing application")

	controllerutil.RemoveFinalizer(app, finalizer)
	err := r.Update(ctx, app)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("Error removing finalizer %v", err)
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apertureiov1alpha1.Application{}).
		Complete(r)
}
