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

	CERT = `-----BEGIN CERTIFICATE-----
	MIIDczCCAlugAwIBAgIRAOswRhuvtiDv7PPjqd1rp4AwDQYJKoZIhvcNAQELBQAw
	JjEkMCIGA1UEAwwba3ViZS1jc3Itc2lnbmVyX0AxNjY0ODgxNTA2MB4XDTIyMTEw
	NDIyMzIwMloXDTIzMTEwNDIyMzIwMlowXTEVMBMGA1UEChMMc3lzdGVtOm5vZGVz
	MUQwQgYDVQQDDDtzeXN0ZW06bm9kZToqLnN0b3JhZ2UtaGwudGVrdG9uLXJlc3Vs
	dHMtMi5zdmMuY2x1c3Rlci5sb2NhbDBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IA
	BLqWtt8TuAjFu4wbQ7zjJ3CftrC0kg2FQwy4rNl8DKpdRCUbcODe/WEBz8+grd2f
	bRwOmPspqobuKYEGyUtFmB2jggEuMIIBKjAOBgNVHQ8BAf8EBAMCBaAwEwYDVR0l
	BAwwCgYIKwYBBQUHAwEwDAYDVR0TAQH/BAIwADAfBgNVHSMEGDAWgBQ3wBL5+86L
	AXNpZIIkXxxWIKfrRjCB0wYDVR0RBIHLMIHIgj5zdG9yYWdlLXBvb2wtMC0wLnN0
	b3JhZ2UtaGwudGVrdG9uLXJlc3VsdHMtMi5zdmMuY2x1c3Rlci5sb2NhbIIobWlu
	aW8udGVrdG9uLXJlc3VsdHMtMi5zdmMuY2x1c3Rlci5sb2NhbIIWbWluaW8udGVr
	dG9uLXJlc3VsdHMtMoIabWluaW8udGVrdG9uLXJlc3VsdHMtMi5zdmOCAiougiQq
	LnRla3Rvbi1yZXN1bHRzLTIuc3ZjLmNsdXN0ZXIubG9jYWwwDQYJKoZIhvcNAQEL
	BQADggEBAIGaajmd6+R32kDIbD2CVAXOGhfDzqL8YUUxoljIRR0u7R48exniFwYI
	tTmgoFDbSjNj3lai8hWSlxiWUYF9MlhDF7MuPLaTVk5swTQolqjwttR2H6hqZRzo
	vP0LoKRx1bPO3YElGEHkS58wMpAExYNJSpI9RGL89POFgPyxmjM1tp5aMw0uJ1RJ
	y9c+sJRiXY5c864QAiVvj7S7Q0Mv/x/z94No4szEtBZx5U76mYkr6pF4lnX+plpu
	/3FdERCdsFh9kmvjRFa8TufG4pd64vHAEVnWARKjxa9TVpAVhSXL7AM6A5o7KILD
	uo632z9zz58/NQHdWOnJbcUydE0p6j8=
	-----END CERTIFICATE-----
	`
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


	host := "minio.tekton-results-2.svc.cluster.local"
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
	rootCAs.AppendCertsFromPEM([]byte(CERT))

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
