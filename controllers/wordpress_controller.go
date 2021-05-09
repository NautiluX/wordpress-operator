/*
Copyright 2021.

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
	"fmt"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	wpv1alpha1 "github.com/NautiluX/wordpress-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sethvargo/go-password/password"
)

// WordpressReconciler reconciles a Wordpress object
type WordpressReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=wp.ntlx.org,resources=wordpresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=wp.ntlx.org,resources=wordpresses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=wp.ntlx.org,resources=wordpresses/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Wordpress object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *WordpressReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("wordpress", req.NamespacedName)

	wordpress := &wpv1alpha1.Wordpress{}
	err := r.Get(ctx, req.NamespacedName, wordpress)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.EnsureSecret(wordpress)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.EnsureDatabase(wordpress)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.EnsureDatabaseService(wordpress)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.EnsureWordpress(wordpress)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *WordpressReconciler) EnsureSecret(wp *wpv1alpha1.Wordpress) error {
	secret := v1.Secret{}
	secretName := types.NamespacedName{Namespace: wp.Namespace, Name: wp.Name}
	err := r.Get(context.TODO(), secretName, &secret)
	if !errors.IsNotFound(err) {
		return err
	}

	if errors.IsNotFound(err) {
		pwd, err := password.Generate(64, 10, 0, false, true)
		if err != nil {
			return err
		}
		rootPwd, err := password.Generate(64, 10, 0, false, true)
		if err != nil {
			return err
		}
		adminPwd, err := password.Generate(64, 10, 0, false, true)
		if err != nil {
			return err
		}
		expectedSecret := v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName.Name,
				Namespace: secretName.Namespace,
			},

			Data: map[string][]byte{
				"DB_HOSTNAME":    []byte(fmt.Sprintf("%s-db:3306", wp.Name)),
				"DB_USER":        []byte("wordpress"),
				"DB_NAME":        []byte("wordpress"),
				"DB_PASSWORD":    []byte(pwd),
				"ROOT_PASSWORD":  []byte(rootPwd),
				"ADMIN_PASSWORD": []byte(adminPwd),
			},
		}
		err = r.Create(context.TODO(), &expectedSecret)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *WordpressReconciler) EnsureDatabase(wp *wpv1alpha1.Wordpress) error {
	deploymentLabels := map[string]string{
		"wordpress-name":    wp.Name,
		"wordpress-db-name": wp.Name + "-db",
	}

	expectedDeployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      wp.Name + "-db",
			Namespace: wp.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: deploymentLabels,
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: deploymentLabels,
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Image: "mysql:5.7",
							Name:  "db",
							Env: []v1.EnvVar{
								{
									Name: "MYSQL_DATABASE",
									ValueFrom: &v1.EnvVarSource{
										SecretKeyRef: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{Name: wp.Name},
											Key:                  "DB_NAME",
										},
									},
								},
								{
									Name: "MYSQL_PASSWORD",
									ValueFrom: &v1.EnvVarSource{
										SecretKeyRef: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{Name: wp.Name},
											Key:                  "DB_PASSWORD",
										},
									},
								},
								{
									Name: "MYSQL_USER",
									ValueFrom: &v1.EnvVarSource{
										SecretKeyRef: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{Name: wp.Name},
											Key:                  "DB_USER",
										},
									},
								},
								{
									Name: "MYSQL_ROOT_PASSWORD",
									ValueFrom: &v1.EnvVarSource{
										SecretKeyRef: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{Name: wp.Name},
											Key:                  "ROOT_PASSWORD",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	deployment := appsv1.Deployment{}
	err := r.Get(context.TODO(), types.NamespacedName{Name: expectedDeployment.Name, Namespace: expectedDeployment.Namespace}, &deployment)
	if !errors.IsNotFound(err) {
		return err
	}
	if errors.IsNotFound(err) {
		err = r.Create(context.TODO(), &expectedDeployment)
		if err != nil {
			return err
		}
	}

	return nil
}
func (r *WordpressReconciler) EnsureDatabaseService(wp *wpv1alpha1.Wordpress) error {
	deploymentLabels := map[string]string{
		"wordpress-name":    wp.Name,
		"wordpress-db-name": wp.Name + "-db",
	}
	service := v1.Service{}
	err := r.Get(context.TODO(), types.NamespacedName{Name: wp.Name + "-db", Namespace: wp.Namespace}, &service)
	if !errors.IsNotFound(err) {
		return err
	}

	expectedService := v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      wp.Name + "-db",
			Namespace: wp.Namespace,
		},
		Spec: v1.ServiceSpec{
			Selector: deploymentLabels,
			Ports: []v1.ServicePort{
				{
					TargetPort: intstr.FromInt(3306),
					Port:       3306,
				},
			},
		},
	}
	err = r.Create(context.TODO(), &expectedService)
	if err != nil {
		return err
	}

	return nil
}

func (r *WordpressReconciler) EnsureWordpress(wp *wpv1alpha1.Wordpress) error {
	deploymentLabels := map[string]string{
		"wordpress-name":    wp.Name,
		"wordpress-wp-name": wp.Name + "-wp",
	}

	expectedDeployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      wp.Name + "-wp",
			Namespace: wp.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: deploymentLabels,
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: deploymentLabels,
				},
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Image:   "manueldewald/wordpress:latest",
							Name:    "init",
							Command: []string{"sh", "-x", "-c", "wp core download --allow-root; wp config create --allow-root --dbname=\"$WORDPRESS_DB_NAME\" --dbuser=\"$WORDPRESS_DB_USER\" --dbpass=\"$WORDPRESS_DB_PASSWORD\" --dbhost=\"$WORDPRESS_DB_HOST\"; wp core install --url=" + wp.Spec.URL + " --title=\"New Page\" --admin_user=\"admin\" --admin_email=\"test@example.com\" --skip-email --allow-root --admin_password=\"$ADMIN_PASSWORD\""},
							Env: []v1.EnvVar{
								{
									Name: "WORDPRESS_DB_HOST",
									ValueFrom: &v1.EnvVarSource{
										SecretKeyRef: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{Name: wp.Name},
											Key:                  "DB_HOSTNAME",
										},
									},
								},
								{
									Name: "WORDPRESS_DB_PASSWORD",
									ValueFrom: &v1.EnvVarSource{
										SecretKeyRef: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{Name: wp.Name},
											Key:                  "DB_PASSWORD",
										},
									},
								},
								{
									Name: "WORDPRESS_DB_NAME",
									ValueFrom: &v1.EnvVarSource{
										SecretKeyRef: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{Name: wp.Name},
											Key:                  "DB_NAME",
										},
									},
								},
								{
									Name: "WORDPRESS_DB_USER",
									ValueFrom: &v1.EnvVarSource{
										SecretKeyRef: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{Name: wp.Name},
											Key:                  "DB_USER",
										},
									},
								},
								{
									Name: "ADMIN_PASSWORD",
									ValueFrom: &v1.EnvVarSource{
										SecretKeyRef: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{Name: wp.Name},
											Key:                  "ADMIN_PASSWORD",
										},
									},
								},
							},
						},
					},
					Containers: []v1.Container{
						{
							Image: "manueldewald/wordpress:latest",
							Name:  "wp",
							Env: []v1.EnvVar{
								{
									Name: "WORDPRESS_DB_HOST",
									ValueFrom: &v1.EnvVarSource{
										SecretKeyRef: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{Name: wp.Name},
											Key:                  "DB_HOSTNAME",
										},
									},
								},
								{
									Name: "WORDPRESS_DB_PASSWORD",
									ValueFrom: &v1.EnvVarSource{
										SecretKeyRef: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{Name: wp.Name},
											Key:                  "DB_PASSWORD",
										},
									},
								},
								{
									Name: "WORDPRESS_DB_NAME",
									ValueFrom: &v1.EnvVarSource{
										SecretKeyRef: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{Name: wp.Name},
											Key:                  "DB_NAME",
										},
									},
								},
								{
									Name: "WORDPRESS_DB_USER",
									ValueFrom: &v1.EnvVarSource{
										SecretKeyRef: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{Name: wp.Name},
											Key:                  "DB_USER",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	deployment := appsv1.Deployment{}
	err := r.Get(context.TODO(), types.NamespacedName{Name: expectedDeployment.Name, Namespace: expectedDeployment.Namespace}, &deployment)
	if !errors.IsNotFound(err) {
		return err
	}
	if errors.IsNotFound(err) {
		err = r.Create(context.TODO(), &expectedDeployment)
		if err != nil {
			return err
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *WordpressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&wpv1alpha1.Wordpress{}).
		Complete(r)
}
