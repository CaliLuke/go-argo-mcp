# Argo Workflows API schema provenance

- Version: `v3.7.3`
- Source: <https://raw.githubusercontent.com/argoproj/argo-workflows/v3.7.3/api/openapi-spec/swagger.json>
- Retrieved: 2026-09-22
- SHA-256: `0672f8d10a280621f3eccf4a84248c5c772c88d6ddea9c63c88090255294022d`

Verify the checked-in source with:

```bash
shasum -a 256 api/argo/v3.7.3/swagger.json
```

The generated Go models are a deliberately small projection selected by
`api/argo/projection.json`. They are not a complete copy of the Argo API.
