#!/bin/bash

# Script to validate Kubernetes manifests without connecting to any cluster or external services
# This script only checks YAML syntax and kustomize structure

set -e

echo "🔍 Validating Kubernetes manifests (offline validation only)..."

# Create a temporary directory for validation
TEMP_DIR=$(mktemp -d)
trap "rm -rf $TEMP_DIR" EXIT

# Function to validate YAML syntax (basic checks)
validate_yaml() {
    local file="$1"
    local filename=$(basename "$file")
    
    # Check if file exists and is readable
    if [ ! -f "$file" ]; then
        echo "❌ $file - File not found"
        return 1
    fi
    
    # Skip kustomize configuration files (they're not Kubernetes resources)
    if [[ "$filename" == "kustomization.yaml" || "$filename" == "kustomizeconfig.yaml" ]]; then
        echo "✅ $file - Kustomize config file"
        return 0
    fi
    
    # Basic YAML structure validation for Kubernetes resources
    if grep -q "apiVersion:" "$file" && grep -q "kind:" "$file"; then
        echo "✅ $file - Valid Kubernetes resource"
        return 0
    else
        echo "❌ $file - Missing required Kubernetes resource fields"
        return 1
    fi
}

# Function to check if kustomize can build (without kubectl)
validate_kustomize() {
    local dir="$1"
    local output_file="$TEMP_DIR/$(basename $dir)-output.yaml"
    
    if command -v kustomize >/dev/null 2>&1; then
        if kustomize build "$dir" > "$output_file" 2>/dev/null; then
            echo "✅ $dir - Kustomize build successful"
            
            # Check for duplicate resource names
            local duplicates=$(grep -E "^  name:" "$output_file" | sort | uniq -d)
            if [ -n "$duplicates" ]; then
                echo "⚠️  $dir - Found potential duplicate resource names:"
                echo "$duplicates"
            fi
            
            return 0
        else
            echo "❌ $dir - Kustomize build failed"
            return 1
        fi
    else
        echo "⚠️  Kustomize not found, skipping build validation for $dir"
        return 0
    fi
}

# Function to check for resource name conflicts
check_resource_conflicts() {
    local dir="$1"
    local output_file="$TEMP_DIR/$(basename $dir)-output.yaml"
    
    if [ -f "$output_file" ]; then
        echo "🔍 Checking for resource conflicts in $dir..."
        
        # Create a temporary file for processing
        local temp_file="$TEMP_DIR/resource_analysis.txt"
        
        # Extract kind and name pairs correctly
        awk '
        /^kind:/ { kind = $2; next }
        /^metadata:/ { in_metadata = 1; next }
        in_metadata && /^  name:/ { 
            name = $2; 
            print kind ":" name; 
            in_metadata = 0 
        }
        /^---/ { kind = ""; in_metadata = 0 }
        ' "$output_file" > "$temp_file"
        
        # Check for duplicates
        sort "$temp_file" | uniq -c | sort -nr > "$temp_file.counts"
        
        local conflicts=$(awk '$1 > 1 {print $2 " (count: " $1 ")"}' "$temp_file.counts")
        if [ -n "$conflicts" ]; then
            echo "❌ Found resource conflicts:"
            echo "$conflicts"
            return 1
        else
            echo "✅ No resource conflicts found"
            return 0
        fi
    fi
}

echo ""
echo "📁 Validating individual YAML files..."

# Validate all YAML files individually
find config/ -name "*.yaml" -type f | while read file; do
    validate_yaml "$file"
done

echo ""
echo "📦 Validating kustomize configurations..."

# Validate each kustomize directory
for dir in config/rbac config/manager config/default; do
    if [ -d "$dir" ]; then
        echo ""
        echo "--- Validating $dir ---"
        validate_kustomize "$dir"
        check_resource_conflicts "$dir"
    fi
done

echo ""
echo "🔍 Checking for common issues..."

# Check for missing files referenced in kustomizations
find config/ -name "kustomization.yaml" -type f | while read kustomfile; do
    dir=$(dirname "$kustomfile")
    echo "Checking references in $kustomfile..."
    
    # Extract resource references
    awk '/^resources:/,/^[^ ]/ {if ($0 ~ /^- /) print $2}' "$kustomfile" | while read resource; do
        if [[ "$resource" != ../* ]]; then
            if [ ! -f "$dir/$resource" ]; then
                echo "❌ Missing file: $dir/$resource (referenced in $kustomfile)"
            fi
        fi
    done
    
    # Extract patch references
    awk '/^patches:/,/^[^ ]/ {if ($0 ~ /^- path:/) print $3}' "$kustomfile" | while read patch; do
        if [ ! -f "$dir/$patch" ]; then
            echo "❌ Missing patch file: $dir/$patch (referenced in $kustomfile)"
        fi
    done
done

echo ""
echo "📋 Summary of manifest structure:"
echo "Manager files:"
find config/manager -name "*.yaml" -type f | sort

echo ""
echo "RBAC files:"
find config/rbac -name "*.yaml" -type f | sort

echo ""
echo "Manager files:"
find config/manager -name "*.yaml" -type f | sort

echo ""
echo "Default files:"
find config/default -name "*.yaml" -type f | sort

echo ""
echo "🎉 Validation complete!"
echo "Next steps for deployment on your VM:"
echo "1. Copy the entire config/ directory to your VM"
echo "2. Run: kubectl apply -f config/default/namespace.yaml"
echo "3. Run: kubectl apply -k config/rbac/"
echo "4. Run: kubectl apply -k config/manager/"
echo "5. Update deployment with your VM's Vault address"
