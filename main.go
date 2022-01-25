package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog"

	"github.com/jetstack/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"github.com/jetstack/cert-manager/pkg/acme/webhook/cmd"
	cmmeta "github.com/jetstack/cert-manager/pkg/apis/meta/v1"
)

const (
	defaultTTL = 600
)

// GroupName groupname
var GroupName = os.Getenv("GROUP_NAME")

func main() {
	if GroupName == "" {
		panic("GROUP_NAME must be specified")
	}

	// This will register our dode DNS provider with the webhook serving
	// library, making it available as an API under the provided GroupName.
	// You can register multiple DNS provider implementations with a single
	// webhook, where the Name() method will be used to disambiguate between
	// the different implementations.
	cmd.RunWebhookServer(GroupName,
		&dodeDNSProviderSolver{},
	)
}

// DodeAPIURL represents the API endpoint to call.
const DodeAPIURL = "https://www.do.de/api/letsencrypt"

// dodeDNSProviderSolver implements the provider-specific logic needed to
// 'present' an ACME challenge TXT record for your own DNS provider.
// To do so, it must implement the `github.com/jetstack/cert-manager/pkg/acme/webhook.Solver`
// interface.
type dodeDNSProviderSolver struct {
	client *kubernetes.Clientset
}

// dodeDNSProviderConfig is a structure that is used to decode into when
// solving a DNS01 challenge.
// This information is provided by cert-manager, and may be a reference to
// additional configuration that's needed to solve the challenge for this
// particular certificate or issuer.
// This typically includes references to Secret resources containing DNS
// provider credentials, in cases where a 'multi-tenant' DNS solver is being
// created.
// If you do *not* require per-issuer or per-certificate configuration to be
// provided to your webhook, you can skip decoding altogether in favour of
// using CLI flags or similar to provide configuration.
// You should not include sensitive information here. If credentials need to
// be used by your provider here, you should reference a Kubernetes Secret
// resource and fetch these credentials using a Kubernetes clientset.
type dodeDNSProviderConfig struct {
	APITokenSecretRef cmmeta.SecretKeySelector `json:"apiTokenSecretRef"`
}

// Name is used as the name for this DNS solver when referencing it on the ACME
// Issuer resource.
// This should be unique **within the group name**, i.e. you can have two
// solvers configured with the same Name() **so long as they do not co-exist
// within a single webhook deployment**.
func (c *dodeDNSProviderSolver) Name() string {
	return "dode"
}

// Present is responsible for actually presenting the DNS record with the
// DNS provider.
// This method should tolerate being called multiple times with the same value.
// cert-manager itself will later perform a self check to ensure that the
// solver has correctly configured the DNS provider.
func (c *dodeDNSProviderSolver) Present(ch *v1alpha1.ChallengeRequest) error {
	cfg, err := loadConfig(ch.Config)
	if err != nil {
		klog.Errorf("Failed to log config %v: %v", ch.Config, err)
		return err
	}
	apiKey, err := c.getAPIKey(&cfg, ch.ResourceNamespace)
	if err != nil {
		klog.Errorf("Failed to get API key %v: %v", ch.Config, err)
		return err
	}
	_, err = c.makeRequest("GET", fmt.Sprintf("?token=%s&domain=%s&value=%s", apiKey, c.removeDOT(ch.ResolvedFQDN), ch.Key))
	if err != nil {
		return err
	}

	return nil
}

// CleanUp should delete the relevant TXT record from the DNS provider console.
// If multiple TXT records exist with the same record name (e.g.
// _acme-challenge.example.com) then **only** the record with the same `key`
// value provided on the ChallengeRequest should be cleaned up.
// This is in order to facilitate multiple DNS validations for the same domain
// concurrently.
func (c *dodeDNSProviderSolver) CleanUp(ch *v1alpha1.ChallengeRequest) error {
	cfg, err := loadConfig(ch.Config)
	if err != nil {
		klog.Errorf("Failed to log config %v: %v", ch.Config, err)
		return err
	}
	apiKey, err := c.getAPIKey(&cfg, ch.ResourceNamespace)
	if err != nil {
		klog.Errorf("Failed to get API key %v: %v", ch.Config, err)
		return err
	}
	_, err = c.makeRequest("GET", fmt.Sprintf("?token=%s&domain=%s&action=delete", apiKey, c.removeDOT(ch.ResolvedFQDN)))
	if err != nil {
		return err
	}

	return nil
}

// Initialize will be called when the webhook first starts.
// This method can be used to instantiate the webhook, i.e. initialising
// connections or warming up caches.
// Typically, the kubeClientConfig parameter is used to build a Kubernetes
// client that can be used to fetch resources from the Kubernetes API, e.g.
// Secret resources containing credentials used to authenticate with DNS
// provider accounts.
// The stopCh can be used to handle early termination of the webhook, in cases
// where a SIGTERM or similar signal is sent to the webhook process.
func (c *dodeDNSProviderSolver) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	cl, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		klog.Errorf("Failed to new kubernetes client: %v", err)
		return err
	}
	c.client = cl

	return nil
}

// loadConfig is a small helper function that decodes JSON configuration into
// the typed config struct.
func loadConfig(cfgJSON *extapi.JSON) (dodeDNSProviderConfig, error) {
	cfg := dodeDNSProviderConfig{}
	// handle the 'base case' where no configuration has been provided
	if cfgJSON == nil {
		return cfg, nil
	}
	if err := json.Unmarshal(cfgJSON.Raw, &cfg); err != nil {
		return cfg, fmt.Errorf("error decoding solver config: %v", err)
	}

	return cfg, nil
}

// Get DODE API key from Kubernetes secret.
func (c *dodeDNSProviderSolver) getAPIKey(cfg *dodeDNSProviderConfig, namespace string) (string, error) {
	secretName := cfg.APITokenSecretRef.Name

	klog.V(6).Infof("try to load secret `%s` with key `%s`", secretName, cfg.APITokenSecretRef.Key)

	sec, err := c.client.CoreV1().Secrets(namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("unable to get secret `%s`; %v", secretName, err)
	}

	secBytes, ok := sec.Data[cfg.APITokenSecretRef.Key]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret \"%s/%s\"", cfg.APITokenSecretRef.Key,
			cfg.APITokenSecretRef.Name, namespace)
	}

	apiKey := string(secBytes)
	return apiKey, nil
}

func (c *dodeDNSProviderSolver) makeRequest(method, uri string) (bool, error) {

	// APIResponse represents a response from DODE API
	type APIResponse struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}

	client := http.Client{
		Timeout: 30 * time.Second,
	}

	url := fmt.Sprintf("%s%s", DodeAPIURL, uri)
	resp, err := client.Get(url)
	if err != nil {
		return false, fmt.Errorf("Error querying DODE API for %s %q -> %v", method, url, err)
	}

	defer resp.Body.Close()

	var r APIResponse
	err = json.NewDecoder(resp.Body).Decode(&r)
	if err != nil {
		return false, err
	}

	if !r.Success {
		return false, fmt.Errorf("DODE API error for %s %q %s", method, uri, r.Error)
	}

	return r.Success, nil
}

func (c *dodeDNSProviderSolver) removeDOT(fqdnURL string) string {
	if strings.HasSuffix(fqdnURL, ".") {
		return strings.TrimSuffix(fqdnURL, ".")
	}
	return fqdnURL
}
