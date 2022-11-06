package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/minio/madmin-go"
	"github.com/minio/minio-go/v7/pkg/credentials"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// OperatorTLSSecretName is the name of secret created with Operator TLS certs
	OperatorTLSSecretName = "operator-tls"
	// OperatorCATLSSecretName is the name of the secret for the operator CA
	OperatorCATLSSecretName = "operator-ca-tls"
)

func main() {
	// add user with a 20 seconds timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*20)
	defer cancel()
	consoleAccessKey := "console"

	// remove spaces and line breaks from access key
	userAccessKey := strings.TrimSpace(string(consoleAccessKey))

	consoleSecretKey := "console123"
	// remove spaces and line breaks from secret key
	userSecretKey := strings.TrimSpace(string(consoleSecretKey))


	host := "https://minio.tekton-results-2.svc.cluster.local"
	accessKey := "minio"
	secretKey := "minio123"

	opts := &madmin.Options{
		Secure: true,
		Creds:  credentials.NewStaticV4(string(accessKey), string(secretKey), ""),
	}

	madmClnt, err := madmin.NewWithOptions(host, opts)
	if err != nil {
		log.Fatal(err)
	}
	madmClnt.SetCustomTransport(getTransport())

	fmt.Println("Add user ====")
	if err := madmClnt.AddUser(ctx, userAccessKey, userSecretKey); err != nil {
		fmt.Printf("Add user error %v\n", err)
		log.Fatal(err)
	}
}

func getTransport() *http.Transport {
	cfg, err := rest.InClusterConfig()

	if err != nil {
		log.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	kubeClientSet, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("Error building Kubernetes clientset: %s", err.Error())
	}

	rootCAs := MustGetSystemCertPool()
	// Default kubernetes CA certificate
	rootCAs.AppendCertsFromPEM(GetPodCAFromFile())

	// If ca.crt exists in operator-tls (ie if the cert was issued by cert-manager) load the ca certificate from there
	operatorTLSCert, err := kubeClientSet.CoreV1().Secrets(GetNSFromFile()).Get(context.Background(), OperatorTLSSecretName, metav1.GetOptions{})
	if err == nil && operatorTLSCert != nil {
		// default secret keys for Opaque k8s secret
		caCertKey := "public.crt"
		// if secret type is k8s tls or cert-manager use the right ca key
		if operatorTLSCert.Type == "kubernetes.io/tls" {
			caCertKey = "tls.crt"
		} else if operatorTLSCert.Type == "cert-manager.io/v1alpha2" || operatorTLSCert.Type == "cert-manager.io/v1" {
			caCertKey = "ca.crt"
		}
		if val, ok := operatorTLSCert.Data[caCertKey]; ok {
			rootCAs.AppendCertsFromPEM(val)
		}
	}

	// Custom ca certificate to be used by operator
	operatorCATLSCert, err := kubeClientSet.CoreV1().Secrets(GetNSFromFile()).Get(context.Background(), OperatorCATLSSecretName, metav1.GetOptions{})
	if err == nil && operatorCATLSCert != nil {
		if val, ok := operatorCATLSCert.Data["tls.crt"]; ok {
			rootCAs.AppendCertsFromPEM(val)
		}
		if val, ok := operatorCATLSCert.Data["ca.crt"]; ok {
			rootCAs.AppendCertsFromPEM(val)
		}
		if val, ok := operatorCATLSCert.Data["public.crt"]; ok {
			rootCAs.AppendCertsFromPEM(val)
		}
	}

	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 15 * time.Second,
		}).DialContext,
		MaxIdleConnsPerHost:   1024,
		IdleConnTimeout:       15 * time.Second,
		ResponseHeaderTimeout: 15 * time.Minute,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 15 * time.Second,
		// Go net/http automatically unzip if content-type is
		// gzip disable this feature, as we are always interested
		// in raw stream.
		DisableCompression: true,
		TLSClientConfig: &tls.Config{
			// Can't use SSLv3 because of POODLE and BEAST
			// Can't use TLSv1.0 because of POODLE and BEAST using CBC cipher
			// Can't use TLSv1.1 because of RC4 cipher usage
			MinVersion: tls.VersionTLS12,
			RootCAs:    rootCAs,
		},
	}
}

func GetNSFromFile() string {
	namespace, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "minio-operator"
	}
	return string(namespace)
}

// GetPodCAFromFile assumes the operator is running inside a k8s pod and extract the
// current ca certificate from /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
func GetPodCAFromFile() []byte {
	namespace, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
	if err != nil {
		return nil
	}
	return namespace
}

// MustGetSystemCertPool - return system CAs or empty pool in case of error (or windows)
func MustGetSystemCertPool() *x509.CertPool {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return x509.NewCertPool()
	}
	return pool
}
