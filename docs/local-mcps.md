# Local MCPs

GoClaw can keep product-specific MCP servers in this repo and run them as optional
sidecars. The MCP remains a separate adapter boundary, but deployment no longer
requires a separate VPS or external repository.

## Facebook MCP

The bundled Facebook MCP lives at:

```text
mcps/facebook
```

Start it with GoClaw:

```bash
./prepare-env.sh
docker compose up -d --build
```

`prepare-env.sh` writes `COMPOSE_FILE` into `.env` and automatically includes
bundled local MCP overlays such as `docker-compose.facebook-mcp.yml`.

Then add the MCP server in the GoClaw UI:

```text
Transport: SSE
URL: http://facebook-mcp:3100/sse
```

Configure page credentials, watermark settings, and scheduled comment settings
in the GoClaw MCP UI as before. GoClaw sends those settings to the bundled MCP
through request headers, so the MCP image does not need page tokens baked into
the container.

## Google Drive MCP

The bundled Google Drive MCP lives at:

```text
mcps/google-drive
```

After `./prepare-env.sh`, start GoClaw normally:

```bash
docker compose up -d --build
```

Then add the MCP server in the GoClaw UI:

```text
Transport: SSE
URL: http://google-drive-mcp:3200/sse
MCP type: Google Drive
```

Configure OAuth refresh-token credentials and the root folder ID in the UI. One
Google Drive MCP server should represent one permission scope, such as one
brand, client, or product asset folder. Tools only list and download image files
inside that configured root folder subtree.
