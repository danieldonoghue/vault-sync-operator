# Vault Sync Operator Documentation

This directory contains detailed documentation for the Vault Sync Operator.

## Documents

### [Project Summary](PROJECT_SUMMARY.md)
Complete development summary including architecture, features, and implementation details.

### [Multi-Cluster Deployment Guide](multi-cluster-deployment.md)
Comprehensive guide for deploying the operator across multiple Kubernetes clusters with shared Vault infrastructure.

### [Secret Rotation Detection](secret-rotation-detection.md)
Intelligent detection and handling of Kubernetes secret changes to optimize performance and reduce Vault load.

### [Performance Optimizations](performance-optimizations.md)
Rate limiting, batch operations, and memory optimization features for high-scale deployments.

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
