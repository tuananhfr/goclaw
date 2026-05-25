# Facebook MCP Server

Standalone MCP Server for Facebook Page management via Graph API v25.0.  
Designed to work with GoClaw (or any MCP client) via SSE transport.

## Architecture

```
GoClaw Agent                    Facebook MCP Server              Facebook
┌──────────┐   SSE + Headers   ┌─────────────────┐   HTTPS     ┌──────────┐
│          │ ─────────────────► │                 │ ──────────► │ Graph API│
│  Agent   │   Authorization:  │  Express + MCP  │             │  v25.0   │
│  Pipeline│   Bearer <token>  │  Server Factory │ ◄────────── │          │
│          │ ◄───────────────── │                 │             └──────────┘
└──────────┘   Tool results    └─────────────────┘
```

**Key design:** One McpServer instance per SSE connection. Each connection receives its own `GraphClient` bound to the session's Facebook token/page. Fully stateless, fully isolated.

## Tools (18 total)

| Tool | Description |
|------|-------------|
| `fb_create_post` | Create text post (with optional link) |
| `fb_create_post_with_media` | Post with multiple uploaded photos |
| `fb_edit_post` | Edit post message |
| `fb_delete_post` | Delete a post |
| `fb_schedule_post` | Schedule future publication |
| `fb_get_posts` | List recent page posts |
| `fb_upload_photo` | Upload photo (unpublished, for multi-photo posts) |
| `fb_create_photo_post` | Post single photo with caption |
| `fb_get_watermark_config` | Get Page watermark config for GoClaw-side preview/rendering |
| `fb_apply_watermark` | Apply configured watermark for backward-compatible manual preview |
| `fb_get_comment_schedule_config` | Get Page scheduled-comment policy for GoClaw-side scheduling |
| `fb_get_comments` | Get comments on a post |
| `fb_create_post_comment` | Create a top-level comment on a post |
| `fb_reply_comment` | Reply to a comment |
| `fb_delete_comment` | Delete a comment |
| `fb_hide_comment` | Hide/unhide a comment |
| `fb_get_post_insights` | Post analytics (impressions, clicks, etc.) |
| `fb_get_page_info` | Page details (name, fans, category) |
| `fb_get_rate_limit` | Check API rate limit usage |

## Quick Start

```bash
# Install
npm install

# Development
npm run dev

# Production
npm run build && npm start
```

## Docker

```bash
docker build -t facebook-mcp .
docker run -p 3100:3100 facebook-mcp
```

## GoClaw Integration

Add to your GoClaw MCP server config (via UI or DB):

```json
{
  "name": "facebook-marketing",
  "transport": "sse",
  "url": "http://facebook-mcp:3100/sse",
  "api_key": "<your_fb_page_access_token>"
}
```

Set `X-Facebook-Page-ID` header in the server headers config, or pass the page ID via GoClaw's MCP server settings.

## Facebook Permissions Required

| Permission | Purpose |
|------------|---------|
| `pages_manage_posts` | Create, edit, delete posts |
| `pages_read_engagement` | Read post insights/analytics |
| `pages_read_user_content` | Read comments |
| `pages_manage_engagement` | Reply to/delete/hide comments |

> In **Development Mode**, your app has full access to Pages managed by the app admin — no review needed.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `3100` | Server listen port |
| `FB_PAGE_ACCESS_TOKEN` | — | Fallback token (when no header provided) |
| `FB_PAGE_ID` | — | Fallback page ID |
| `FB_WATERMARK` | - | Fallback JSON watermark config; use `fb_get_watermark_config` and GoClaw `apply_watermark` for preview before posting |
| `FB_COMMENT_SCHEDULE` | - | Fallback JSON scheduled-comment policy; use `fb_get_comment_schedule_config` and GoClaw scheduling |
| `FB_API_VERSION` | `v25.0` | Graph API version |
