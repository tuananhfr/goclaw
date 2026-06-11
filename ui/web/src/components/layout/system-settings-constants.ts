/** Curated 1024-dimension embedding models per provider type. */
export const EMBEDDING_MODELS: Record<string, { id: string; name: string }[]> = {
  openai_compat: [
    { id: "bge-m3", name: "BGE-M3 (1024d, recommended local)" },
    { id: "BAAI/bge-m3", name: "BAAI/bge-m3 (1024d)" },
  ],
  openrouter: [
    { id: "jina/jina-embeddings-v3", name: "jina/jina-embeddings-v3 (1024d)" },
  ],
  gemini_native: [
    { id: "gemini-embedding-001", name: "gemini-embedding-001 (3072d -> 1024 via dimensions)" },
  ],
  mistral: [
    { id: "codestral-embed", name: "codestral-embed (requires 1024 dimensions override)" },
  ],
  dashscope: [
    { id: "text-embedding-v3", name: "text-embedding-v3 (1024 via dimensions)" },
  ],
  cohere: [
    { id: "embed-v4", name: "embed-v4 (requires 1024 dimensions override)" },
  ],
};

export const DEFAULT_EMBEDDING_MODELS: { id: string; name: string }[] = [];

export interface InitState {
  embProvider: string;
  embModel: string;
  embMaxChunkLen: string;
  embChunkOverlap: string;
  toolStatus: boolean;
  blockReply: boolean;
  intentClassify: boolean;
  compProvider: string;
  compModel: string;
  compThreshold: string;
  compKeepRecent: string;
  compMaxTokens: string;
  kgProvider: string;
  kgModel: string;
  kgMinConfidence: string;
  bgProvider: string;
  bgModel: string;
}

export const DEFAULTS: InitState = {
  embProvider: "", embModel: "",
  embMaxChunkLen: "", embChunkOverlap: "",
  toolStatus: true, blockReply: false, intentClassify: true,
  compProvider: "", compModel: "",
  compThreshold: "", compKeepRecent: "", compMaxTokens: "",
  kgProvider: "", kgModel: "", kgMinConfidence: "0.75",
  bgProvider: "", bgModel: "",
};

export function parseBool(v: string | undefined, fallback: boolean): boolean {
  if (v === undefined) return fallback;
  return v !== "false" && v !== "0";
}
