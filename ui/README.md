# Log Monitor UI

React and TypeScript frontend for the Log Monitor application. It is built as
an independent container and served by Nginx. Nginx proxies `/api` requests to
the internal `log-api-service`, so the browser only needs the UI service URL.

## Local development

```bash
npm install
npm run dev
```

Vite proxies `/api` to `http://localhost:8080` during development.

## Kubernetes access

```bash
kubectl port-forward -n log-monitor svc/log-monitor-ui 3000:8080
```

Open `http://localhost:3000` and use `admin` / `admin`.
