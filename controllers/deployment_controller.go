/*
Copyright 2022.

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

package controllers

import (
	"context"
	"encoding/json"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

// DeploymentReconciler reconciles a Deployment object
type DeploymentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	SimpleCopierAnnotationImgReplace string = "knowshan.github.io/SimpleCopier-image-replace"
	SimpleCopierAnnotationImgVer     string = "knowshan.github.io/SimpleCopier-image-version"
	SimpleCopierFinalizer            string = "knowshan.github.io/SimpleCopier-finalizer"
)

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=pods/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Pod object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *DeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("deployment", req.NamespacedName)

	var deployment = &appsv1.Deployment{}
	if err := r.Get(ctx, req.NamespacedName, deployment); err != nil {
		logger.Error(err, "Unable to fetch Deployment information")
		return ctrl.Result{}, err
	}

	logger.V(10).Info("Received reconcile request", "Deployment", deployment.String())

	copierSuffix, ok := deployment.ObjectMeta.Annotations[SimpleCopierAnnotationImgVer]
	if ok {
		logger.Info("Found Copier Annotation Img Ver")

		// Remove metadata added after creation
		// Add copier image version as suffix to labels so that original service labels don't select copied deployment
		var copierMetadata = &metav1.ObjectMeta{}
		copierMetadata = deployment.ObjectMeta.DeepCopy()
		var copierDeploymentSpec = &appsv1.DeploymentSpec{}
		copierDeploymentSpec = deployment.Spec.DeepCopy()
		// ToDo error check
		err := copiedObjMetadata(copierMetadata, copierSuffix)
		var copierDeployment appsv1.Deployment = appsv1.Deployment{Spec: *copierDeploymentSpec, ObjectMeta: *copierMetadata}
		var copierReplicas int32 = 1
		copierDeployment.Spec.Replicas = &copierReplicas

		deployment.ObjectMeta.GetAnnotations()
		// Image Replace
		imgReplaceAnnotation, ok := deployment.ObjectMeta.Annotations[SimpleCopierAnnotationImgReplace]
		if ok {
			logger.Info("Received Img Replace Map", "imgReplaceMap", imgReplaceAnnotation)

			// Sample Annotation - anntn := "\"{\\\"nginx:1.14.2\\\" : \\\"nginx:latest\\\"}\"\n"
			// Remove trailing newline, then remove extra quotes, finally remove backslashes
			// ToDo: Figure out better / elegant manner to transform JSON string later
			imgReplaceAnnotationParsed := strings.Trim(imgReplaceAnnotation, "\n")
			imgReplaceAnnotationParsed = strings.Trim(imgReplaceAnnotationParsed, "\"")
			imgReplaceAnnotationParsed = strings.ReplaceAll(imgReplaceAnnotationParsed, "\\", "")
			// q := json.RawMessage(imgReplaceAnnotationParsed[0 : len(imgReplaceAnnotationParsed)-2])
			logger.V(10).Info("Image replace annotation", "imgReplaceAnnotationRaw", imgReplaceAnnotation, "imgReplaceAnnotationParsed", imgReplaceAnnotationParsed)

			// ToDo: Better variable names
			// v := map[string]interface{}{}
			// err = json.Unmarshal([]byte(q), &v)
			imgReplaceMap := map[string]interface{}{}
			if err := json.Unmarshal([]byte(imgReplaceAnnotationParsed), &imgReplaceMap); err != nil {
				logger.Error(err, "JSON parsing error for image replace annotation", "imgReplaceAnnotation", imgReplaceAnnotation, "imgReplaceMap", imgReplaceMap)
			}
			for i, container := range copierDeployment.Spec.Template.Spec.Containers {
				newImage, imgKeyFound := imgReplaceMap[container.Image]
				if imgKeyFound {
					copierDeployment.Spec.Template.Spec.Containers[i].Image = newImage.(string)
					logger.Info("Replacing image", "originalImage", container.Image, "copierImage", newImage)
				}
			}
			for k, v := range copierDeployment.Spec.Selector.MatchLabels {
				copierDeployment.Spec.Selector.MatchLabels[k] = v + "-" + copierSuffix
			}
			for k, v := range copierDeployment.Spec.Template.Labels {
				copierDeployment.Spec.Template.Labels[k] = v + "-" + copierSuffix
			}
		}

		// Create copy of deployment if Finalizer is absent - New Deployment to process
		// ToDo: Create if Not Exists condition
		if !controllerutil.ContainsFinalizer(deployment, SimpleCopierFinalizer) {
			err = r.Client.Create(ctx, &copierDeployment)
			if err == nil {
				logger.Info("Created deployment copy", "copierDeployment", copierDeployment.Name)
			} else {
				logger.Error(err, "Failed to create deployment copy", "meta", "copierMetadata")
			}
		}
		// Check if the Deployment instance is marked to be deleted, which is
		// indicated by the deletion timestamp being set.
		isDeploymentMarkedToBeDeleted := deployment.GetDeletionTimestamp() != nil
		if isDeploymentMarkedToBeDeleted {
			if controllerutil.ContainsFinalizer(deployment, SimpleCopierFinalizer) {
				// Run finalization logic for deployment - Delete Deployment Copy
				// If the finalization logic fails, don't remove the finalizer so that we can retry during the next reconciliation.
				if err := r.finalizeDeployment(logger, ctx, &copierDeployment); err != nil {
					logger.Error(err, "Failed to remove deployment copy")
					return ctrl.Result{}, err
				}

				// Remove SimpleCopierFinalizer on Deployment
				// Once all finalizers have been removed, the object will be deleted.
				controllerutil.RemoveFinalizer(deployment, SimpleCopierFinalizer)
				err := r.Update(ctx, deployment)
				if err != nil {
					return ctrl.Result{}, err
				} else {
					logger.Info("Removed SimpleCopier finalizer")
				}
			}
			return ctrl.Result{}, nil
		}

		// Add finalizer
		if !controllerutil.ContainsFinalizer(deployment, SimpleCopierFinalizer) {
			r.Client.Get(ctx, req.NamespacedName, deployment)
			controllerutil.AddFinalizer(deployment, SimpleCopierFinalizer)
			err := r.Client.Update(ctx, deployment)
			if err != nil {
				logger.Error(err, "Failed to add finalizer for following deployment.")
				return ctrl.Result{}, err
			} else {
				logger.Info("Added SimpleCopier finalizer for following deployment.")
			}
		}

	} else {
		logger.Info("Not Found SimpleCopier Annotation for Img Ver")
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.Deployment{}).
		Complete(r)
}

// ToDo: Split into multiple funcs (add suffix, delete keys) and move to utils
func copiedObjMetadata(metadata *metav1.ObjectMeta, copierSuffix string) error {

	var annotationsToBeDeleted = []string{"creationTimestamp", "resourceVersion", "uid", "deployment.kubernetes.io/revision", "kubectl.kubernetes.io/last-applied-configuration", SimpleCopierAnnotationImgVer}
	for _, a := range annotationsToBeDeleted {
		delete(metadata.Annotations, a)
	}
	metadata.Name = metadata.Name + "-" + copierSuffix
	metadata.ResourceVersion = ""
	for k, v := range metadata.Labels {
		metadata.Labels[k] = v + "-" + copierSuffix
	}
	return nil
}

func (r *DeploymentReconciler) finalizeDeployment(logger logr.Logger, c context.Context, d *appsv1.Deployment) error {
	logger.Info("Deleting SimpleCopier Deployment as part of teardown", "SimpleCopierDeploymentName", d.Name)
	var deletionObj = &appsv1.Deployment{}
	var deletionNamespacedName = types.NamespacedName{Namespace: d.Namespace, Name: d.Name}
	// ToDo: Error Handling
	r.Client.Get(c, deletionNamespacedName, deletionObj)
	logger.V(10).Info("SimpleCopier deletion object that will be passed to delete", "SimpleCopierDeployment", deletionObj.String())

	// Delete if name not empty - Need to check as sometimes Get returned object without any metadata
	// Reason: 2022-01-05T15:17:32.541-0800    ERROR   controller.deployment   **** Failed To Delete copier Deployment ***     {"reconciler group": "apps", "reconciler kind": "Deployment", "name": "nginx-deployment-test", "namespace": "default", "deployment": "default/nginx-deployment-test", "error": "resource name may not be empty"}
	if deletionObj.ObjectMeta.Name != "" {
		err := r.Client.Delete(c, deletionObj)
		if err != nil {
			logger.Error(err, "Failed To Delete SimpleCopierDeployment")
			return err
		} else {
			return nil
		}
	} else {
		logger.Info("Received empty metadata for SimpleCopierDeployment during teardown")
		return nil
	}
}
