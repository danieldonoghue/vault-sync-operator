# Vault Sync Operator Documentation

This directory contains detailed documentation for the Vault Sync Operator.

## Documents

### [Project Summary](PROJECT_SUMMARY.md)
Complete development summary including architecture, features, and implementation details.

### [Multi-Cluster Deployment Guide](multi-cluster-deployment.md)
Comprehensive guide for deploying the operator across multiple Kubernetes clusters with shared Vault infrastructure.

## Quick Links

- [Main README](../README.md) - Getting started and basic usage
- [Examples](../examples/) - Example deployment configurations
- [Configuration](../config/) - Kubernetes manifests and configuration files

## Architecture Overview

The Vault Sync Operator follows standard Kubernetes operator patterns:

```
┌─────────────────────────────────────────────────────────────┐
│                    Kubernetes Cluster                       │
│                                                             │
│  ┌─────────────────┐    ┌─────────────────┐                │
│  │   Deployment    │    │   Deployment    │                │
│  │   with Vault    │    │   with Vault    │                │
│  │   Annotations   │    │   Annotations   │                │
│  └─────────────────┘    └─────────────────┘                │
│           │                       │                        │
│           │       watches         │                        │
│           └───────────┼───────────┘                        │
│                       │                                    │
│           ┌─────────────────┐                              │
│           │  Vault Sync     │                              │
│           │  Operator       │                              │
│           └─────────────────┘                              │
│                       │                                    │
└───────────────────────┼────────────────────────────────────┘
                        │
            ┌─────────────────┐
            │  HashiCorp      │
            │  Vault Server   │
            └─────────────────┘
```

The operator watches for deployments with `vault-sync.io/path` annotations and automatically syncs referenced secrets to Vault using Kubernetes authentication.
