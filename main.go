package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func checkAuth(r *http.Request) bool {
	authKey := os.Getenv("API_KEY")
	bearer := r.Header.Get("Authorization")
	prefix := "bearer "
	token := ""

	if strings.HasPrefix(strings.ToLower(bearer), prefix) {
		token = bearer[len(prefix):]
	}

	return token == authKey
}

func main() {
	authKey := os.Getenv("API_KEY")
	if authKey == "" {
		log.Fatalf("API_KEY not set")
	}

	tlsCertFile := os.Getenv("TLS_CERT_FILE")
	tlsKeyFile := os.Getenv("TLS_KEY_FILE")
	if tlsCertFile == "" || tlsKeyFile == "" {
		log.Fatalf("TLS_CERT_FILE and TLS_KEY_FILE not set")
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Error creating in-cluster config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating Kubernetes client: %v", err)
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating dynamic client: %v", err)
	}

	http.HandleFunc("/api/v1/pods", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		pods, err := getPods(clientset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		transformPodList(dynamicClient, pods)

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(pods); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	http.HandleFunc("/api/v1/services", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		svcs, err := getServices(clientset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(svcs); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	http.HandleFunc("/api/v1/nodes", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		nodes, err := getNodes(clientset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(nodes); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "443"
	}

	log.Printf("Server starting on port %s", port)
	log.Fatal(http.ListenAndServeTLS(":"+port, tlsCertFile, tlsKeyFile, nil))
}

func getPods(clientset *kubernetes.Clientset) (*v1.PodList, error) {
	podList, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error fetching pods: %v", err)
	}

	return podList, nil
}

func getServices(clientset *kubernetes.Clientset) (*v1.ServiceList, error) {
	serviceList, err := clientset.CoreV1().Services("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error fetching services: %v", err)
	}

	return serviceList, nil
}

func getNodes(clientset *kubernetes.Clientset) (*v1.NodeList, error) {
	nodeList, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error fetching nodes: %v", err)
	}

	return nodeList, nil
}

func findVmiForPod(pod *v1.Pod, vmis *unstructured.UnstructuredList) map[string]interface{} {
	if vmis == nil {
		return nil
	}

	ownerRefs := pod.OwnerReferences
	ownerVmiName := ""
	for _, ownerRef := range ownerRefs {
		if ownerRef.Kind == "VirtualMachineInstance" {
			ownerVmiName = ownerRef.Name
			break
		}
	}

	if ownerVmiName == "" {
		return nil
	}

	var foundVMI map[string]interface{} = nil

	for _, vmi := range vmis.Items {
		vmiObj := vmi.Object

		vmiMetadata, ok := vmiObj["metadata"].(map[string]interface{})
		if !ok {
			continue
		}

		vmiName, ok := vmiMetadata["name"].(string)
		if !ok {
			continue
		}

		vmiNamespace, ok := vmiMetadata["namespace"].(string)
		if !ok {
			continue
		}

		if pod.Namespace == vmiNamespace && vmiName == ownerVmiName {
			foundVMI = vmiObj
			break
		}
	}

	return foundVMI
}

func getMacToIPMappingForVMI(vmi map[string]interface{}) map[string]string {
	vmiStatus, ok := vmi["status"].(map[string]interface{})
	if !ok {
		return nil
	}

	vmiInterfaces, ok := vmiStatus["interfaces"].([]interface{})
	if !ok {
		return nil
	}

	mapping := make(map[string]string)

	for _, vmiInterface := range vmiInterfaces {
		vmiInterfaceM, ok := vmiInterface.(map[string]interface{})
		if !ok {
			continue
		}

		vmiIfaceMac, ok := vmiInterfaceM["mac"].(string)
		if !ok || vmiIfaceMac == "" {
			continue
		}

		vmiIp, ok := vmiInterfaceM["ipAddress"].(string)
		if !ok || vmiIp == "" {
			continue
		}

		mapping[vmiIfaceMac] = vmiIp
	}

	return mapping
}

func transformPodList(dynamicClient *dynamic.DynamicClient, podList *v1.PodList) {
	vmiGvr := schema.GroupVersionResource{
		Group:    "kubevirt.io",
		Version:  "v1",
		Resource: "virtualmachineinstances",
	}

	vmis, err := dynamicClient.Resource(vmiGvr).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		// Assume we don't have any VMIs or can't read them
		log.Printf("Couldnt get VMIs: %v", err)
		vmis = nil
	}

	for _, pod := range podList.Items {
		networkStatus, ok := pod.Annotations["k8s.v1.cni.cncf.io/network-status"]
		if !ok {
			continue
		}

		var networks []map[string]interface{}

		json.Unmarshal([]byte(networkStatus), &networks)

		for _, network := range networks {
			_, ok := network["ips"]
			if ok {
				// Already has IP(s)
				continue
			}
			network["ips"] = []string{}

			if vmis == nil {
				continue
			}

			foundVMI := findVmiForPod(&pod, vmis)
			if foundVMI == nil {
				continue
			}

			addressMapping := getMacToIPMappingForVMI(foundVMI)
			if addressMapping == nil {
				continue
			}

			networkMac, ok := network["mac"].(string)
			if !ok || networkMac == "" {
				continue
			}

			vmiIp, ok := addressMapping[networkMac]
			if !ok {
				continue
			}

			network["ips"] = append(network["ips"].([]string), vmiIp)
		}

		newNetworksJSON, err := json.Marshal(networks)
		if err != nil {
			log.Printf("Error marshalling networks: %v", err)
			continue
		}

		pod.Annotations["k8s.v1.cni.cncf.io/network-status"] = string(newNetworksJSON)
	}
}
