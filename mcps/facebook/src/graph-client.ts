/**
 * Facebook Graph API v25.0 client.
 * Stateless — receives token + pageId per instance (one per SSE session).
 */
import * as path from "path";
import { applyWatermark, type WatermarkConfig } from "./watermark.js";

const DEFAULT_API_VERSION = "v25.0";

export interface GraphErrorDetail {
  message: string;
  type: string;
  code: number;
  error_subcode?: number;
  fbtrace_id?: string;
}

export class GraphAPIError extends Error {
  constructor(
    public readonly detail: GraphErrorDetail,
    public readonly statusCode: number,
  ) {
    super(`[${detail.code}] ${detail.type}: ${detail.message}`);
    this.name = "GraphAPIError";
  }
}

export interface RateLimitInfo {
  callCount: number;
  totalCpuTime: number;
  totalTime: number;
}

export class GraphClient {
  private readonly baseUrl: string;
  private lastRateLimit: RateLimitInfo | null = null;

  constructor(
    private readonly accessToken: string,
    private readonly pageId: string,
    private readonly goclawBaseUrl?: string,
    private readonly goclawToken?: string,
    apiVersion: string = DEFAULT_API_VERSION,
    private readonly watermark?: WatermarkConfig,
  ) {
    this.baseUrl = `https://graph.facebook.com/${apiVersion}`;
  }

  getPageId(): string {
    return this.pageId;
  }

  getRateLimit(): RateLimitInfo | null {
    return this.lastRateLimit;
  }

  async get(endpoint: string, params: Record<string, string> = {}): Promise<any> {
    const url = new URL(`${this.baseUrl}/${endpoint}`);
    url.searchParams.set("access_token", this.accessToken);
    for (const [k, v] of Object.entries(params)) {
      url.searchParams.set(k, v);
    }

    const res = await fetch(url.toString());
    return this.handleResponse(res);
  }

  async post(endpoint: string, params: Record<string, string> = {}): Promise<any> {
    const url = new URL(`${this.baseUrl}/${endpoint}`);

    const body = new URLSearchParams();
    body.set("access_token", this.accessToken);
    for (const [k, v] of Object.entries(params)) {
      body.set(k, v);
    }

    const res = await fetch(url.toString(), {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: body.toString(),
    });
    return this.handleResponse(res);
  }

  async postMultipart(endpoint: string, formData: FormData): Promise<any> {
    const url = new URL(`${this.baseUrl}/${endpoint}`);
    url.searchParams.set("access_token", this.accessToken);

    const res = await fetch(url.toString(), {
      method: "POST",
      body: formData,
    });
    return this.handleResponse(res);
  }

  private async downloadImage(imageUrl: string): Promise<{ blob: Blob; filename: string }> {
    // Convert local file paths to GoClaw HTTP URLs for cross-machine support.
    // GoClaw creates files at /app/workspace/... inside its container.
    // This MCP server may run on a different machine, so we fetch via HTTP.
    if (this.isLocalFileRef(imageUrl)) {
      return this.fetchFromGoClaw(imageUrl);
    }

    console.log(`[GraphClient] Downloading image from: ${imageUrl}`);
    const res = await fetch(imageUrl);
    if (!res.ok) {
      throw new Error(`Failed to download image: HTTP ${res.status} from ${imageUrl}`);
    }

    const contentType = res.headers.get("content-type") ?? "image/jpeg";
    const arrayBuffer = await res.arrayBuffer();
    const sizeKB = Math.round(arrayBuffer.byteLength / 1024);
    console.log(`[GraphClient] Downloaded: ${sizeKB}KB, type: ${contentType}`);

    // Facebook limits: 4MB general, 1MB for PNG
    const maxBytes = contentType.includes("png") ? 1 * 1024 * 1024 : 4 * 1024 * 1024;
    if (arrayBuffer.byteLength > maxBytes) {
      const maxMB = maxBytes / (1024 * 1024);
      throw new Error(`Image too large (${sizeKB}KB). Facebook limit: ${maxMB}MB for ${contentType}`);
    }

    if (arrayBuffer.byteLength === 0) {
      throw new Error(`Downloaded image is empty (0 bytes) from ${imageUrl}`);
    }

    const blob = new Blob([arrayBuffer], { type: contentType });
    const ext = contentType.includes("png") ? "png" : contentType.includes("webp") ? "webp" : "jpg";
    const filename = `upload.${ext}`;

    return { blob, filename };
  }

  /**
   * Fetch image from GoClaw's /v1/files endpoint.
   * Converts file:///app/workspace/... → GOCLAW_BASE_URL/v1/files/app/workspace/...
   * Requires GOCLAW_BASE_URL and GOCLAW_GATEWAY_TOKEN env vars.
   */
  private async fetchFromGoClaw(filePath: string): Promise<{ blob: Blob; filename: string }> {
    const baseUrl = this.goclawBaseUrl || process.env.GOCLAW_BASE_URL;
    const token = this.goclawToken || process.env.GOCLAW_GATEWAY_TOKEN;

    if (!baseUrl) {
      throw new Error(
        `Cannot fetch local file "${filePath}" — GOCLAW_BASE_URL is not set. ` +
        `Pass x-goclaw-base-url in MCP headers, or set GOCLAW_BASE_URL environment variable.`
      );
    }

    // Normalize: file:///app/workspace/... → app/workspace/...
    let relativePath = filePath.replace(/\\/g, "/");
    if (relativePath.startsWith("file:///")) {
      relativePath = relativePath.slice("file:///".length);
    } else if (relativePath.startsWith("/")) {
      relativePath = relativePath.slice(1);
    }

    const url = `${baseUrl.replace(/\/$/, "")}/v1/files/${relativePath}`;
    console.log(`[GraphClient] Fetching from GoClaw: ${url}`);

    const headers: Record<string, string> = {};
    if (token) {
      headers["Authorization"] = `Bearer ${token}`;
    }

    const res = await fetch(url, { headers });
    if (!res.ok) {
      throw new Error(`Failed to fetch image from GoClaw: HTTP ${res.status} — ${url}`);
    }

    const contentType = res.headers.get("content-type") ?? "image/jpeg";
    const arrayBuffer = await res.arrayBuffer();
    const sizeKB = Math.round(arrayBuffer.byteLength / 1024);
    console.log(`[GraphClient] Fetched from GoClaw: ${sizeKB}KB, type: ${contentType}`);

    const blob = new Blob([arrayBuffer], { type: contentType });
    const ext = path.extname(relativePath).toLowerCase().replace(".", "") || "jpg";
    const filename = path.basename(relativePath) || `upload.${ext}`;

    return { blob, filename };
  }

  private async fetchBufferFromGoClaw(filePath: string): Promise<{ buffer: Buffer; contentType: string; filename: string }> {
    const { blob, filename } = await this.fetchFromGoClaw(filePath);
    const arrayBuffer = await blob.arrayBuffer();
    return { buffer: Buffer.from(arrayBuffer), contentType: blob.type || "image/jpeg", filename };
  }

  async loadAssetBuffer(pathOrUrl: string): Promise<{ buffer: Buffer; contentType: string; filename: string }> {
    if (pathOrUrl.startsWith("MEDIA:")) pathOrUrl = pathOrUrl.substring(6);
    if (pathOrUrl.startsWith("/v1/files/")) {
      const baseUrl = this.goclawBaseUrl || process.env.GOCLAW_BASE_URL;
      if (!baseUrl) {
        throw new Error(`Cannot fetch GoClaw file "${pathOrUrl}" â€” GOCLAW_BASE_URL is not set.`);
      }
      const token = this.goclawToken || process.env.GOCLAW_GATEWAY_TOKEN;
      const headers: Record<string, string> = {};
      if (token) headers["Authorization"] = `Bearer ${token}`;
      const res = await fetch(`${baseUrl.replace(/\/$/, "")}${pathOrUrl}`, { headers });
      if (!res.ok) throw new Error(`Failed to fetch GoClaw asset: HTTP ${res.status}`);
      const contentType = res.headers.get("content-type") ?? "application/octet-stream";
      const buffer = Buffer.from(await res.arrayBuffer());
      return { buffer, contentType, filename: path.basename(pathOrUrl) || "asset" };
    }
    if (this.isLocalFileRef(pathOrUrl)) {
      return this.fetchBufferFromGoClaw(pathOrUrl);
    }
    const headers: Record<string, string> = {};
    const baseUrl = this.goclawBaseUrl || process.env.GOCLAW_BASE_URL;
    if (baseUrl && pathOrUrl.startsWith(baseUrl.replace(/\/$/, "")) && (this.goclawToken || process.env.GOCLAW_GATEWAY_TOKEN)) {
      headers["Authorization"] = `Bearer ${this.goclawToken || process.env.GOCLAW_GATEWAY_TOKEN}`;
    }
    const res = await fetch(pathOrUrl, { headers });
    if (!res.ok) throw new Error(`Failed to fetch asset: HTTP ${res.status}`);
    const contentType = res.headers.get("content-type") ?? "application/octet-stream";
    const buffer = Buffer.from(await res.arrayBuffer());
    const pathname = new URL(pathOrUrl).pathname;
    return { buffer, contentType, filename: path.basename(pathname) || "asset" };
  }

  private async loadWatermarkAsset(pathOrUrl: string): Promise<Buffer> {
    if (pathOrUrl.startsWith("MEDIA:")) pathOrUrl = pathOrUrl.substring(6);
    if (pathOrUrl.startsWith("/v1/files/")) {
      const baseUrl = this.goclawBaseUrl || process.env.GOCLAW_BASE_URL;
      if (!baseUrl) {
        throw new Error(`Cannot fetch GoClaw file "${pathOrUrl}" — GOCLAW_BASE_URL is not set.`);
      }
      const token = this.goclawToken || process.env.GOCLAW_GATEWAY_TOKEN;
      const headers: Record<string, string> = {};
      if (token) headers["Authorization"] = `Bearer ${token}`;
      const res = await fetch(`${baseUrl.replace(/\/$/, "")}${pathOrUrl}`, { headers });
      if (!res.ok) throw new Error(`Failed to fetch watermark asset: HTTP ${res.status}`);
      return Buffer.from(await res.arrayBuffer());
    }
    if (this.isLocalFileRef(pathOrUrl)) {
      const { buffer } = await this.fetchBufferFromGoClaw(pathOrUrl);
      return buffer;
    }
    const headers: Record<string, string> = {};
    const baseUrl = this.goclawBaseUrl || process.env.GOCLAW_BASE_URL;
    if (baseUrl && pathOrUrl.startsWith(baseUrl.replace(/\/$/, "")) && (this.goclawToken || process.env.GOCLAW_GATEWAY_TOKEN)) {
      headers["Authorization"] = `Bearer ${this.goclawToken || process.env.GOCLAW_GATEWAY_TOKEN}`;
    }
    const res = await fetch(pathOrUrl, { headers });
    if (!res.ok) throw new Error(`Failed to fetch watermark asset: HTTP ${res.status}`);
    return Buffer.from(await res.arrayBuffer());
  }

  private async maybeWatermark(blob: Blob, filename: string): Promise<{ blob: Blob; filename: string }> {
    if (!this.watermark?.enabled) {
      console.log(`[Watermark] skipped page=${this.pageId} reason=disabled-or-missing`);
      return { blob, filename };
    }
    const itemCount = this.watermark.items?.length ?? 1;
    const ref = this.watermark.logo_url || this.watermark.logo_path || (this.watermark.mode === "text" ? "text" : "");
    console.log(`[Watermark] applying page=${this.pageId} items=${itemCount} mode=${this.watermark.mode ?? "-"} ref=${ref ? "set" : "-"}`);
    const input = Buffer.from(await blob.arrayBuffer());
    const out = await applyWatermark(input, this.watermark, (ref) => this.loadWatermarkAsset(ref));
    console.log(`[Watermark] applied page=${this.pageId} input=${input.length} output=${out.data.length}`);
    return {
      blob: new Blob([new Uint8Array(out.data)], { type: out.contentType }),
      filename: filename.replace(/\.[^.]+$/, "") + "-watermarked.jpg",
    };
  }

  async applyWatermarkToImage(imageUrl: string): Promise<{ data: string; contentType: string; filename: string; sizeBytes: number }> {
    if (imageUrl.startsWith("MEDIA:")) imageUrl = imageUrl.substring(6);
    let { blob, filename } = await this.downloadImage(imageUrl);
    ({ blob, filename } = await this.maybeWatermark(blob, filename));
    const buffer = Buffer.from(await blob.arrayBuffer());
    return {
      data: buffer.toString("base64"),
      contentType: blob.type || "image/jpeg",
      filename,
      sizeBytes: buffer.length,
    };
  }

  async delete(endpoint: string): Promise<any> {
    const url = new URL(`${this.baseUrl}/${endpoint}`);
    url.searchParams.set("access_token", this.accessToken);

    const res = await fetch(url.toString(), { method: "DELETE" });
    return this.handleResponse(res);
  }

  private async handleResponse(res: Response): Promise<any> {
    this.parseRateLimitHeader(res);
    const data = await res.json() as any;

    if (data.error) {
      throw new GraphAPIError(data.error, res.status);
    }
    return data;
  }

  private parseRateLimitHeader(res: Response): void {
    const header = res.headers.get("x-business-use-case-usage");
    if (!header) return;

    try {
      const usage = JSON.parse(header);
      const pageUsage = usage[this.pageId];
      if (Array.isArray(pageUsage) && pageUsage.length > 0) {
        this.lastRateLimit = {
          callCount: pageUsage[0].call_count ?? 0,
          totalCpuTime: pageUsage[0].total_cputime ?? 0,
          totalTime: pageUsage[0].total_time ?? 0,
        };
      }
    } catch {
      // Ignore parse errors
    }
  }

  // ── Post operations ──

  async createPost(message: string, link?: string): Promise<any> {
    const params: Record<string, string> = { message };
    if (link) params.link = link;
    return this.post(`${this.pageId}/feed`, params);
  }

  async createPostWithMedia(message: string, mediaIds: string[]): Promise<any> {
    const params: Record<string, string> = { message };
    mediaIds.forEach((id, i) => {
      params[`attached_media[${i}]`] = JSON.stringify({ media_fbid: id });
    });
    return this.post(`${this.pageId}/feed`, params);
  }

  async editPost(postId: string, message: string): Promise<any> {
    return this.post(postId, { message });
  }

  async deletePost(postId: string): Promise<any> {
    return this.delete(postId);
  }

  async schedulePost(message: string, publishTime: number, link?: string): Promise<any> {
    const params: Record<string, string> = {
      message,
      published: "false",
      scheduled_publish_time: String(publishTime),
    };
    if (link) params.link = link;
    return this.post(`${this.pageId}/feed`, params);
  }

  async getPosts(limit: number = 10): Promise<any> {
    return this.get(`${this.pageId}/posts`, {
      fields: "id,message,created_time,permalink_url,shares,full_picture",
      limit: String(limit),
    });
  }

  // ── Photo operations ──

  async uploadPhoto(imageUrl: string, caption?: string, published: boolean = false, applyWatermark: boolean = false): Promise<any> {
    if (imageUrl.startsWith("MEDIA:")) imageUrl = imageUrl.substring(6);
    if (applyWatermark && this.watermark?.enabled) {
      return this.uploadPhotoBinary(imageUrl, caption, published, true);
    }
    if (this.isLocalFileRef(imageUrl)) {
      return this.uploadPhotoBinary(imageUrl, caption, published, applyWatermark);
    }
    
    // Try URL-based upload first
    try {
      const params: Record<string, string> = {
        url: imageUrl,
        published: String(published),
      };
      if (caption) params.caption = caption;
      return await this.post(`${this.pageId}/photos`, params);
    } catch (err) {
      // If Facebook can't fetch the URL (error 324), download and upload binary
      if (err instanceof GraphAPIError && err.detail.code === 324) {
        console.log(`[GraphClient] URL upload failed (324), falling back to binary upload...`);
        return this.uploadPhotoBinary(imageUrl, caption, published, applyWatermark);
      }
      throw err;
    }
  }

  private async uploadPhotoBinary(imageUrl: string, caption?: string, published: boolean = false, applyWatermark: boolean = false): Promise<any> {
    let { blob, filename } = await this.downloadImage(imageUrl);
    if (applyWatermark) {
      ({ blob, filename } = await this.maybeWatermark(blob, filename));
    }
    console.log(`[GraphClient] Binary upload: ${filename}, size: ${blob.size} bytes`);

    const formData = new FormData();
    formData.set("source", blob, filename);
    formData.set("published", String(published));
    if (caption) formData.set("caption", caption);

    return this.postMultipart(`${this.pageId}/photos`, formData);
  }

  async createPhotoPost(imageUrl: string, caption: string, applyWatermark: boolean = false): Promise<any> {
    if (imageUrl.startsWith("MEDIA:")) imageUrl = imageUrl.substring(6);
    if (applyWatermark && this.watermark?.enabled) {
      return this.createPhotoPostBinary(imageUrl, caption, true);
    }
    if (this.isLocalFileRef(imageUrl)) {
      return this.createPhotoPostBinary(imageUrl, caption, applyWatermark);
    }

    // Try URL-based upload first
    try {
      return await this.post(`${this.pageId}/photos`, {
        url: imageUrl,
        caption,
        published: "true",
      });
    } catch (err) {
      // Fallback to binary upload
      if (err instanceof GraphAPIError && err.detail.code === 324) {
        console.log(`[GraphClient] URL upload failed (324), falling back to binary upload...`);
        return this.createPhotoPostBinary(imageUrl, caption, applyWatermark);
      }
      throw err;
    }
  }

  private async createPhotoPostBinary(imageUrl: string, caption: string, applyWatermark: boolean = false): Promise<any> {
    let { blob, filename } = await this.downloadImage(imageUrl);
    if (applyWatermark) {
      ({ blob, filename } = await this.maybeWatermark(blob, filename));
    }
    const formData = new FormData();
    formData.set("source", blob, filename);
    formData.set("caption", caption);
    formData.set("published", "true");
    return this.postMultipart(`${this.pageId}/photos`, formData);
  }

  private isLocalFileRef(value: string): boolean {
    return value.startsWith("file://") || value.startsWith("/") || /^[A-Za-z]:[\\/]/.test(value);
  }

  // ── Comment operations ──

  async getComments(postId: string, limit: number = 25): Promise<any> {
    return this.get(`${postId}/comments`, {
      fields: "id,message,from,created_time,like_count",
      limit: String(limit),
    });
  }

  async replyComment(commentId: string, message: string): Promise<any> {
    return this.post(`${commentId}/comments`, { message });
  }

  async createPostComment(postId: string, message: string): Promise<any> {
    return this.post(`${postId}/comments`, { message });
  }

  async deleteComment(commentId: string): Promise<any> {
    return this.delete(commentId);
  }

  async hideComment(commentId: string, hide: boolean = true): Promise<any> {
    return this.post(commentId, { is_hidden: String(hide) });
  }

  // ── Insights operations ──

  async getPostInsights(postId: string): Promise<any> {
    const metrics = [
      "post_impressions",
      "post_impressions_unique",
      "post_impressions_organic",
      "post_impressions_paid",
      "post_engaged_users",
      "post_clicks",
      "post_reactions_like_total",
    ].join(",");
    return this.get(`${postId}/insights`, { metric: metrics, period: "lifetime" });
  }

  async getPageInfo(): Promise<any> {
    return this.get(this.pageId, {
      fields: "name,about,category,website,fan_count,description,emails,phone,location",
    });
  }
}
