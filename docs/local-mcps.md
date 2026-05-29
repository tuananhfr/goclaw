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

Configure OAuth refresh-token credentials and the root folder ID in the UI. The
Google Drive MCP now supports a shared root folder with per-agent folder grants:
one MCP can serve multiple brand/client folders, while each agent only sees the
folder IDs mapped to its agent key or UUID in `agent_folder_grants`.

When an MCP client connects, the server immediately starts an initial recursive
sync for the root folder, writes a local index under `drive-cache`, and caches
downloaded files by Drive file ID. A daily incremental sync runs at the
configured time (default `00:00`, `Asia/Ho_Chi_Minh`), and `gdrive_sync_now`
can be called to sync changes or a specific folder immediately after uploading
new assets.

The MCP supports both public Drive links and private/shared files that the
connected OAuth account can view. Domain-wide admin delegation is not required.
