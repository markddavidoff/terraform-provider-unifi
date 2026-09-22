# Unifi Terraform Provider (terraform-provider-unifi)

> ## ⚠️ This is a patched fork
>
> Published as **`markddavidoff/unifi`**. Upstream is
> [`ubiquiti-community/terraform-provider-unifi`](https://github.com/ubiquiti-community/terraform-provider-unifi)
> — **use upstream unless you specifically need the fixes below.**
>
> It exists because five defects were found while migrating a live UniFi OS
> controller (Network **10.6.101**, UDR) to Terraform, and upstream `v0.55.0`
> is the newest release, so there was nothing to upgrade to. Every fix is
> unit-tested and verified end-to-end against real hardware.
>
> | Fix | Effect |
> |---|---|
> | `excluded_networkconf_ids` duplicate collapsed on read | a one-shot port-profile create no longer produces an unplannable resource (`Duplicate Set Element`) |
> | `unifi_network` create | no longer fails with an unhandled HTTP 500 at provider defaults |
> | `tagged_networkconf_ids` | now errors at plan time instead of being silently discarded by the controller |
> | 404 on network read | maps to drift, not a hard plan error |
> | POST retry on 5xx (go-unifi) | non-idempotent writes are no longer retried; final error keeps the status code |
>
> Also carries a `replace` onto
> [`markddavidoff/go-unifi`](https://github.com/markddavidoff/go-unifi)
> (`compat/v1.33-lastgood`), because upstream `main` does not compile against
> its own pinned go-unifi `v1.34.x`.
>
> **Versioning:** `0.55.1001` reads as "upstream 0.55 lineage, fork patch
> 1001". The high patch number is deliberate — it marks divergence and sorts
> above any plausible upstream `0.55.x`. Note the fork is cut from upstream
> `main`, which is *past* the `v0.55.0` tag.
>
> **These fixes belong upstream.** PRs are the intended long-term path; this
> fork is a stopgap, not a competing distribution. It is maintained only as
> far as one homelab needs it — no support is implied.

[![Acceptance Tests](https://github.com/ubiquiti-community/terraform-provider-unifi/actions/workflows/acctest.yaml/badge.svg)](https://github.com/ubiquiti-community/terraform-provider-unifi/actions/workflows/acctest.yaml) [![codecov](https://codecov.io/github/ubiquiti-community/terraform-provider-unifi/graph/badge.svg?token=KVP7FS41IG)](https://codecov.io/github/ubiquiti-community/terraform-provider-unifi)

> **Note**: You can't (for obvious reasons) configure your network while connected to something that may disconnect (like the WiFi). Use a hard-wired connection to your controller to use this provider.

Functionality first needs to be added to the [go-unifi](https://github.com/ubiquiti-community/go-unifi) SDK.

## Documentation

You can browse documentation on the [Terraform provider registry](https://registry.terraform.io/providers/ubiquiti-community/unifi/latest/docs).

## Supported Unifi Controller Versions

As of version [v0.34](https://github.com/ubiquiti-community/terraform-provider-unifi/releases/tag/v0.34.0), this provider only supports version 6 of the Unifi controller software. If you need v5 support, you can pin an older version of the provider.

The docker, UDM, and UDM-Pro versions are slightly different (the API is proxied a little differently) but for the most part should all be supported. Individual patch versions of the controller are generally not tested for compatibility, just the latest stable versions.

## Using the Provider

### Terraform 1.0 and above

You can use the provider via the [Terraform provider registry](https://registry.terraform.io/providers/ubiquiti-community/unifi).
