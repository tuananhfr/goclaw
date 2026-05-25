import { GraphClient } from "./graph-client.js";
import type { CommentScheduleConfig } from "./mcp-server.js";
import type { WatermarkConfig } from "./watermark.js";

export interface PageConfig {
  pageId: string;
  name?: string;
  client: GraphClient;
  watermark?: WatermarkConfig;
  commentSchedule?: CommentScheduleConfig;
}

export class PageRegistry {
  private pages = new Map<string, PageConfig>();
  private defaultPageId: string | null = null;

  constructor(private context?: { goclawBaseUrl?: string; goclawToken?: string }) {}

  async addPage(pageId: string, accessToken: string, name?: string, watermark?: WatermarkConfig, commentSchedule?: CommentScheduleConfig): Promise<PageConfig> {
    const client = new GraphClient(accessToken, pageId, this.context?.goclawBaseUrl, this.context?.goclawToken, undefined, watermark);
    
    try {
      // Validate token by fetching page info
      const data = await client.get(pageId, { fields: "name" });
      const resolvedName = name || data.name || pageId;
      
      const config: PageConfig = { pageId, name: resolvedName, client, watermark, commentSchedule };
      this.pages.set(pageId, config);
      
      if (!this.defaultPageId) {
        this.defaultPageId = pageId;
      }
      return config;
    } catch (err) {
      throw new Error(`Failed to validate token for page ${pageId}: ${err instanceof Error ? err.message : String(err)}`);
    }
  }

  // Synchronous add without validation (for initial default page from env)
  addPageSync(pageId: string, accessToken: string, name?: string, watermark?: WatermarkConfig, commentSchedule?: CommentScheduleConfig): void {
    const client = new GraphClient(accessToken, pageId, this.context?.goclawBaseUrl, this.context?.goclawToken, undefined, watermark);
    this.pages.set(pageId, { pageId, name: name || pageId, client, watermark, commentSchedule });
    if (!this.defaultPageId) {
      this.defaultPageId = pageId;
    }
  }

  removePage(pageId: string): boolean {
    const deleted = this.pages.delete(pageId);
    if (deleted && this.defaultPageId === pageId) {
      const remaining = Array.from(this.pages.keys());
      this.defaultPageId = remaining.length > 0 ? remaining[0] : null;
    }
    return deleted;
  }

  listPages(): Array<{ pageId: string; name?: string; isDefault: boolean }> {
    return Array.from(this.pages.values()).map(p => ({
      pageId: p.pageId,
      name: p.name,
      isDefault: p.pageId === this.defaultPageId
    }));
  }

  getWatermarkConfig(pageId?: string): { pageId: string; watermark?: WatermarkConfig } {
    const config = this.getPageConfig(pageId);
    return { pageId: config.pageId, watermark: config.watermark };
  }

  getCommentScheduleConfig(pageId?: string): { pageId: string; commentSchedule?: CommentScheduleConfig } {
    const config = this.getPageConfig(pageId);
    return { pageId: config.pageId, commentSchedule: config.commentSchedule };
  }

  private getPageConfig(pageId?: string): PageConfig {
    const targetId = pageId || this.defaultPageId;
    if (!targetId) {
      throw new Error("No page specified and no default page available. Please add a page first using fb_add_page.");
    }
    const config = this.pages.get(targetId);
    if (!config) {
      throw new Error(`Page ${targetId} not found in registry. Please add it first.`);
    }
    return config;
  }

  setDefaultPage(pageId: string): void {
    if (!this.pages.has(pageId)) {
      throw new Error(`Page ${pageId} not found in registry`);
    }
    this.defaultPageId = pageId;
  }

  getClient(pageId?: string): GraphClient {
    const targetId = pageId || this.defaultPageId;
    if (!targetId) {
      throw new Error("No page specified and no default page available. Please add a page first using fb_add_page.");
    }
    const config = this.pages.get(targetId);
    if (!config) {
      throw new Error(`Page ${targetId} not found in registry. Please add it first.`);
    }
    return config.client;
  }
}
