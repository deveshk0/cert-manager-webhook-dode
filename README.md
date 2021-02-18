# DoDE Webhook for Cert Manager

This is a webhook solver for [DODE](https://www.do.de).

## Prerequisites

* [cert-manager](https://github.com/jetstack/cert-manager) >= 0.11
    - [Installing on Kubernetes](https://docs.cert-manager.io/en/release-0.11/getting-started/install/kubernetes.html)

*Note: use version < 0.3 with cert-manager < 0.11*

## Installation

Generate API Token from dode (https://www.do.de/account/letsencrypt/).

```console
$ helm install --name cert-manager-webhook-dode ./deploy/cert-manager-webhook-dode \
    --namespace <NAMESPACE-WHICH-CERT-MANAGER-INSTALLED> \
    --set groupName=<GROUP_NAME> \
    --set secrets.apiToken=<DODE_API_TOKEN> \
    --set clusterIssuer.enabled=true,clusterIssuer.email=<EMAIL_ADDRESS>
```

### Automatically creating Certificates for Ingress resources

See [this](https://cert-manager.io/docs/usage/ingress/#optional-configuration).
