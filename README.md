# Coraza Kubernetes Operator

[Web Application Firewall (WAF)] support for [Kubernetes] [Gateways].

[Web Application Firewall (WAF)]:https://www.cloudflare.com/learning/ddos/glossary/web-application-firewall-waf/
[Kubernetes]:https://github.com/kubernetes
[Gateways]:https://gateway-api.sigs.k8s.io/api-types/gateway/

## About

The Coraza Kubernetes Operator (CKO) enables declarative management of Web
Application Firewalls (WAF) `Engines` and `RuleSets` in Kubernetes. These
`Engines` are built on [CorazaWAF]. The CKO supports attachment of `Engines`
to `Gateways` and enforcement of rules via `RuleSets`.

[CorazaWAF]:https://github.com/corazawaf/coraza

**Key Features:**

- `Engine` API to declaratively deploy WAF instances
- `RuleSet` API to declaratively provide rules to WAF instances
- Dynamic `RuleSet` updates
- [ModSecurity Seclang] compatibility

[ModSecurity Seclang]:https://github.com/owasp-modsecurity/ModSecurity/wiki/Reference-Manual-(v3.x)

### Supported Integrations

The operator integrates with other tools to attach WAF instances to
their gateways/proxies:

- `istio` - Istio integration ✅ **Currently Supported (ingress Gateway only)**
- `wasm` - WebAssembly deployment ✅ **Currently Supported**

> **Note**: Only Istio+Wasm is supported for now.

### Architecture

The CKO's `RuleSetController` responds to `RuleSet` resources by validating and
compiling the rules, which gets emitted to a cache. The keys for the cache are
the namespace/name of the `RuleSet`, allowing the compiled set of rules to be
polled from a cache server hosting the cache.

> **Note**: Currently, only [Seclang] rules are supported.

The `EngineController` responds to `Engine` resources by deploying a WAF engine
according to the type and mode provided, and attaching it to a `Gateway`. It
targets a `RuleSet` to poll the compiled ruleset from the cache server and apply
it to the `Engine`. Poll intervals are set so the rules can be dynamically
updated over time.

<img width="825" height="460" alt="cko-architecture-diagram" src="https://github.com/user-attachments/assets/e7b257e3-096f-4321-a40d-fe4e473480ac" />

[Seclang]:https://github.com/owasp-modsecurity/ModSecurity/wiki/Reference-Manual-(v3.x)

## Usage

Make sure your supported platform is deployed to the cluster, then choose one
of the installation methods.

> **Note**: For deploying Istio, we recommend the [Sail Operator].

[Sail Operator]:https://github.com/istio-ecosystem/sail-operator/

### Installation

#### Install with Kustomize

```bash
kubectl apply -k config/default
```

#### Install with Helm

TODO

#### Install via OperatorHub

[TODO]

[TODO]:https://github.com/redhat-openshift-ecosystem/community-operators-prod

### Firewall Deployment

Firstly deploy your `RuleSets` which organize all your rules.

> **Note**: Only `ConfigMaps` are supported for rules currently.

Once your `RuleSets` are deployed you can deploy an `Engine` to load and
enforce those rules on a `Gateway`.

> **Note**: Currently can only target an Istio `Gateway` resource.

You can find examples of `RuleSets` and `Engines` in `config/samples/`. The
documentation for these APIs is available in the [API Documentation](todo).

## Contributing

Contributions are welcome!

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache License 2.0 - see [LICENSE](LICENSE).
