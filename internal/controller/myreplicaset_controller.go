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
	"regexp"
	"sort"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/klog/v2"

	appsv1beta1 "devops-test/api/v1beta1"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type TimedPod struct {
	Pod  string
	Time metav1.Time
}

// MyReplicaSetReconciler reconciles a MyReplicaSet object
type MyReplicaSetReconciler struct {
	client.Client

	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=apps.opsblogs.cn,resources=myreplicasets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.opsblogs.cn,resources=myreplicasets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.opsblogs.cn,resources=myreplicasets/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the MyReplicaSet object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *MyReplicaSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Fetch the MyReplicaSet instance
	var myReplicaSet appsv1beta1.MyReplicaSet
	if err := r.Client.Get(ctx, req.NamespacedName, &myReplicaSet); err != nil {
		// Error reading the object - requeue the request.
		klog.Error("get MyReplicaSet failed")
		if errors.IsNotFound(err) {
			// The object no longer exists - remove any finalizers if they exist.
			return reconcile.Result{}, nil
		}
		// Something else went wrong - requeue and report the error.
		return reconcile.Result{}, err
	}

	// Define a new Pod object
	podList := &v1.PodList{}
	labelSelector := myReplicaSet.Spec.Template.ObjectMeta.Labels

	if err := r.Client.List(ctx, podList, client.InNamespace(myReplicaSet.Namespace)); err != nil {
		klog.Error(err, "Failed to list namespaces all  Pods")
		return reconcile.Result{}, err
	} else {
		pods := []string{}
		//times := []metav1.Time{}
		podsTimes := make(map[string]metav1.Time)
		regx := regexp.MustCompile(myReplicaSet.ObjectMeta.Name)
		for _, pod := range podList.Items {
			//查找对应的pod是否是属于这个myreplicaset
			for _, ownerRef := range pod.ObjectMeta.OwnerReferences {
				if ownerRef.Kind == myReplicaSet.Kind && regx.MatchString(ownerRef.Name) && CompareMaps(pod.Labels, labelSelector) {
					fmt.Printf("Found pod %s owned by ReplicaSet %s podLables is %s  labelsSelector %s   \n", pod.Name, ownerRef.Name, pod.Labels, labelSelector)
					pods = append(pods, pod.Name)
					//获取pod信息ldd
					klog.Infof("Found %d Pods , The is %d pods in namespace %s  pods is %s  create_time: %s\n ", len(podList.Items), len(pods), myReplicaSet.Namespace, pods, pod.ObjectMeta.CreationTimestamp)
					//times = append(times, pod.CreationTimestamp)

					podsTimes[pod.Name] = pod.CreationTimestamp
					//}
				}

			}

		}

		//创建pod
		if len(pods) < int(*myReplicaSet.Spec.Replicas) {
			klog.Info("No pods found in podList, ready create pods")
			dep, err := r.podForMyReplicaset(&myReplicaSet, myReplicaSet.Spec)
			if err = r.Create(ctx, dep); err != nil {
				klog.Error(err, "Failed to create new MyReplicaset pods ",
					"MyReplicaset.Namespace ", dep.Namespace, "MyReplicaset.Name ", dep.Name)
				return ctrl.Result{}, err
			}
		}

		//for x, y := range podsTimes {
		//	fmt.Printf("Pod: %v, Time: %s\n", x, y)
		//}

		//删除多余的pod
		// 提取 map 中的元素到切片
		var timedPods []TimedPod
		for pod, time := range podsTimes {
			timedPods = append(timedPods, TimedPod{Pod: pod, Time: time})
		}

		// 根据时间排序
		sort.Slice(timedPods, func(i, j int) bool {
			return timedPods[i].Time.Before(&timedPods[j].Time)
		})

		// 打印排序后的所有结果
		for x, tp := range timedPods {
			fmt.Printf("Number %d ,Time: %v, Pod: %s\n", x, tp.Time, tp.Pod)
		}

		// 检查切片是否为空
		if len(timedPods) > 0 {
			// 打印切片中的最后一个元素
			lastPod := timedPods[len(timedPods)-1]
			klog.Infof("pods number is %d replicase is  %d \n", len(pods), int(*myReplicaSet.Spec.Replicas))

			if len(pods) > int(*myReplicaSet.Spec.Replicas) {
				klog.Infof("ready delete pods,pod name is %s,pods number is %d ", lastPod.Pod, len(pods))

				// 创建一个删除选项，例如设置删除前的宽限期
				gracePeriod := int64(1) // 5秒宽限期

				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      lastPod.Pod,
						Namespace: myReplicaSet.Namespace,
					},
				}
				// 删除Pod
				if err := r.Delete(context.Background(), pod, client.GracePeriodSeconds(gracePeriod)); err != nil {
					klog.Error(err, " Failed to delete new MyReplicaset pods ",
						"MyReplicaset.Namespace ", pod.Namespace, " MyReplicaset.pod.Name ", pod.Name)
					return ctrl.Result{}, err
				}
				klog.Infof("Last Pod: Time - %v, Pod - %s is delete Successful\n", lastPod.Time, lastPod.Pod)

			}
			klog.Infof("Last Pod: Time  %v, Pod: Name  %s \n", lastPod.Time, lastPod.Pod)
		} else {
			klog.Info("The slice is empty no pods")
		}

		// for循每个1s watching myreplicaset
		klog.Info("requeue reconcile: return ctrl.Result{RequeueAfter: time.Second}, nil")
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	// // Set MyReplicaSet instance as the owner and controller
	// if err := controllerutil.SetControllerReference(&myReplicaSet, &v1.Pod{}, r.Scheme); err != nil {
	// 	klog.Info("Set MyReplicaSet instance as the owner and controller")
	// 	return reconcile.Result{}, err
	// }
	// TODO: your custom logic to sync the desired state

	// // Update the found object and write the result back if there are any changes
	// if err := r.Client.Update(ctx, &myReplicaSet); err != nil {
	// 	klog.Info("Update the found object and write the result back if there are any changes")
	// 	return reconcile.Result{}, err
	// }

	// // Periodically requeue reconciliation requests for MyReplicaSet
	// klog.Info("Periodically requeue reconciliation requests for MyReplicaSet")
	// return reconcile.Result{RequeueAfter: time.Second}, nil

}

func labelsForMyReplicaset(myreplicaset appsv1beta1.MyReplicaSetSpec) map[string]string {
	label := myreplicaset.Selector.MatchLabels

	return label
}
func CompareMaps(map1 map[string]string, map2 map[string]string) bool {
	if len(map1) != len(map2) {
		return false
	}
	for key, value := range map1 {
		if value != map2[key] {
			return false
		}
	}
	return true
}

func Rand() string {
	rand.Seed(time.Now().UnixNano())
	// 定义字符集，包含字母和数字
	charset := "abcdefghijklmnopqrstuvwxyz0123456789"

	// 生成5位随机组合
	randomString := make([]byte, 5)
	for i := 0; i < 5; i++ {
		randomString[i] = charset[rand.Intn(len(charset))]
	}

	//fmt.Println("Generated random string:", string(randomString))
	return string(randomString)
}

func (r *MyReplicaSetReconciler) podForMyReplicaset(
	myreplicaset *appsv1beta1.MyReplicaSet, label appsv1beta1.MyReplicaSetSpec) (*v1.Pod, error) {
	ls := labelsForMyReplicaset(label)

	dep := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      myreplicaset.Name + "-" + Rand(),
			Namespace: myreplicaset.Namespace,
			Labels:    ls,
		},
		Spec: v1.PodSpec{
			// TODO(user): Uncomment the following code to configure the nodeAffinity expression
			Containers: myreplicaset.Spec.Template.Spec.Containers,
			Volumes:    myreplicaset.Spec.Template.Spec.Volumes,
		},
	}

	// Set the ownerRef for the Deployment
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/

	if err := ctrl.SetControllerReference(myreplicaset, dep, r.Scheme); err != nil {
		return nil, err
	}

	return dep, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MyReplicaSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1beta1.MyReplicaSet{}).
		Complete(r)
}
