#!/bin/bash
set -euo pipefail

echo "=== Deploying Hytte ==="

cd /home/robin/Hytte

echo "Pulling latest code..."
git pull origin main

echo "Building frontend..."
cd web
npm ci
npm run build
cd ..

echo "Building backend..."
go build -o bin/hytte ./cmd/server

echo "Restarting service..."
sudo systemctl restart hytte

echo "Checking status..."
sleep 2
sudo systemctl is-active hytte

echo "=== Deploy complete ==="
