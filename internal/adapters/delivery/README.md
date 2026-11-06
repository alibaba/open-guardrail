# Built-in delivery context adapters

Status: planned. This directory contains no Go implementation.

Place reusable context-provider implementations here when a delivery platform
requires server-side context lookup. Map its external IDs and checkpoints into
the public delivery contract; keep platform API clients out of domain packages.

Corporate platform SDKs remain in the adopter's module. Deployment execution and
traffic control remain with the CD platform. Pipeline or YAML recipes that simply
call the HTTP API/CLI belong in `integrations`, not in this adapter package.
