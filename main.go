package main

import (
	//"encoding/json"
	"bytes"
	"fmt"
	"github.com/sanity-io/litter"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	//apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	//flag "github.com/spf13/pflag"
	"github.com/spf13/viper"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	//"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	k8Yaml "k8s.io/apimachinery/pkg/util/yaml"
)

var success bool

var seededRand *rand.Rand = rand.New(
	rand.NewSource(time.Now().UnixNano()))

const charset = "abcdefghijklmnopqrstuvwxyz0123456789"

func RandomStringWithCharset(length int, charset string) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

func RandomString(length int) string {
	return RandomStringWithCharset(length, charset)
}

func main() {

	success = false

	viper.BindEnv("namespace")
	viper.SetDefault("Namespace", "leech2")
	namespace := viper.GetString("namespace") // case-insensitive Setting & Getting

	viper.BindEnv("jobName")
	viper.SetDefault("Jobname", fmt.Sprintf("sba-%s", RandomString(5)))
	jobName := viper.GetString("jobName") // case-insensitive Setting & Getting

	viper.BindEnv("jobfile")
	viper.SetDefault("Jobfile", "/downloads/sba_job.json")
	jobFile := viper.GetString("jobfile")

	//flag.Parse()

	fmt.Printf("Namespace: %s\n", namespace)
	fmt.Printf("Jobname: %s\n", jobName)

	fmt.Printf("%#v\n", os.Args)

	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		log.Fatalf("Couldn't get Kubernetes default config: %s", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	//file, err := os.Open(jobFile)
	//if err != nil {
	//	panic(err.Error())
	//}

	jb, err := ioutil.ReadFile(jobFile)

	jerb := batchv1.Job{}
	dec := k8Yaml.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(jb)), 1000)

	if err := dec.Decode(&jerb); err != nil {
		log.Fatalf("Error decoding file: %v", err)
	}

	//dec := json.NewDecoder(file)

	//var jerb batchv1.Job
	//dec.Decode(&jerb)

	litter.Dump(jerb)

	jerb.Spec.Template.Spec.Containers[0].Args = os.Args[1:]
	fmt.Printf("%#v\n", jerb.Spec.Template.Spec.Containers[0].Args)

	jerb.ObjectMeta.Name = jobName
	jerb.ObjectMeta.Labels["job-name"] = jobName
	jerb.Spec.Template.ObjectMeta.Labels["job-name"] = jobName

	jobsClient := clientset.BatchV1().Jobs(namespace)
	result, err := jobsClient.Create(&jerb)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Created job %q.\n", result.GetObjectMeta().GetName())
	fmt.Printf("Setting up the informer\n")

	factory := informers.NewFilteredSharedInformerFactory(
		clientset,
		0,
		namespace,
		func(opt *metav1.ListOptions) {
			opt.LabelSelector = fmt.Sprintf("job-name=%s", jobName)
		},
	)

	informer := factory.Batch().V1().Jobs().Informer()
	stopper := make(chan struct{})
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			// "k8s.io/apimachinery/pkg/apis/meta/v1" provides an Object
			// interface that allows us to get metadata easily
			mObj := obj.(metav1.Object)
			fmt.Printf("New Job Added to Store: %s\n", mObj.GetName())
		},
		DeleteFunc: func(obj interface{}) {
			// We are not using this currentl since we are going to leave the job after its finished
			jobCheck, _ := obj.(*batchv1.Job)

			if jobCheck.Status.Succeeded == 1 {
				success = true
			} else {
				success = false
			}

			close(stopper)
		},
		UpdateFunc: func(oldObj interface{}, newObj interface{}) {
			fmt.Printf("Job updated:\n")
			//oldJob, _ := oldObj.(*batchv1.Job)
			newJob, _ := newObj.(*batchv1.Job)

			//  Here we want to see if the job succeeded or failed (1 = true)
			//  If both are 0, do nothing
			// maybe we should check for Active instead?
			if newJob.Status.Active == 1 {
				fmt.Printf("Doing nothing as job is still active\n")
				return
			}
			fmt.Printf("Checking status of job\n")

			if newJob.ObjectMeta.DeletionTimestamp == nil {
				// let's gather the output from the pod
				fmt.Printf("Job just before deleting\n")
				litter.Dump(newJob)

				fmt.Printf("Deleting job %s from namespace %s\n", newJob.Name, newJob.Namespace)

				//				fg := metav1.DeletePropagationForeground
				//				deleteOptions := metav1.DeleteOptions{PropagationPolicy: &fg}

				//				if err := clientset.BatchV1().Jobs(newJob.Namespace).Delete(newJob.Name, &deleteOptions); err != nil {
				//					fmt.Printf("Failed to delete job: %s\n", err)
				//				}
				fmt.Printf("Job would have been deleted\n")
				if newJob.Status.Succeeded == 1 {
					success = true
				} else {
					success = false
				}
				close(stopper)
			}
		},
	})

	informer.Run(stopper)
	fmt.Printf("success: %v\n", success)
	if success == true {
		os.Exit(0)
	} else {
		os.Exit(1)
	}
}
