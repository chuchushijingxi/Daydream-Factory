import type { ListFilter, ResourceAccess } from './spaces-types';
import type { CreditPriceDTO, UsageBasisDTO } from './administration-types';
export type ProjectSettings = {
  aspectRatio: string;
  llmModel: string;
  imageModel: string;
  videoModel: string;
};
export type Character = {
  roleType: string;
  age: string;
  gender: string;
  biography: string;
  appearance: string;
  personality: string;
};
export type AssetImage = {
  fileId: string;
  originalUrl: string;
  thumbnailUrl: string;
};
export type Look = {
  id: string;
  name: string;
  description: string;
  origin: string;
  image?: AssetImage | null;
};
export type Asset = {
  id: string;
  kind: 'character' | 'location' | 'prop';
  name: string;
  aliases: string[];
  description: string;
  sourceIds: string[];
  origin: string;
  character: Character | null;
  looks: Look[];
  image?: AssetImage | null;
};
export type VisualTarget = {
  assetId: string;
  lookId?: string;
};
export type TaskRun = {
  credit?: UsageBasisDTO | null;
  id: string;
  contract: string;
  sourceVersion: number;
  status: string;
  stage: string;
  error?: string;
  errorCode?: string;
  callCount: number;
  targets?: VisualTarget[];
  target?: VisualTarget;
};
export type VisualStyle = {
  id: string;
  name: string;
  previewUrl: string;
  promptVersion: string;
  description?: string;
  prompts: {
    zh: string;
    en: string;
  };
};
export type Project = {
  spaceId: string;
  ownerId: string;
  creatorId: string;
  id: string;
  title: string;
  uploadedAt: string | null;
  revision: number;
  source: {
    filename: string;
    version: number;
    sha256: string;
    spanCount: number;
  };
  assets: Asset[];
  runs: TaskRun[];
  outline: null | {
    protagonist: string;
    genre: string;
    summary: string;
    background: string;
    logline: string;
  };
  settings: ProjectSettings | null;
  style: VisualStyle | null;
  visualPrompts: {
    origin?: string;
    target: {
      assetId: string;
      lookId?: string;
    };
    text: string;
    stale: boolean;
    sourceVersion: number;
    runId: string;
  }[];
  catalogEditedByUser: boolean;
  usedOrReservedMicroCny: number;
  limitMicroCny: number;
};
export type ScriptSource = {
  version: number;
  filename: string;
  spans: {
    id: string;
    text: string;
  }[];
};
export type ProjectRow = {
  spaceId: string;
  ownerId: string;
  creatorId: string;
  creatorName: string;
  sharedWithMe: boolean;
  effectiveAccess: ResourceAccess;
  id: string;
  title: string;
  filename: string;
  uploadedAt: string | null;
  revision: number;
  previewImages: string[];
  latestRun?: Pick<TaskRun, 'id' | 'contract' | 'status' | 'stage'> | null;
};
export type ServiceStatus = {
  readOnly: boolean;
  configured: boolean;
  capabilities: Record<string, boolean>;
};
export type CharacterImageType =
  | 'closeup'
  | 'main_view'
  | 'closeup_main'
  | 'three_view'
  | 'closeup_three_view'
  | 'no_preset';
export type MediaOptions = { resolution: string; durationSeconds?: number };
export type MediaProfile = {
  supplierPrice: string;
  resolutions: { id: string; label: string; pixels?: Record<string, string> }[];
  defaultResolution: string;
  durations?: number[];
  defaultDuration?: number;
  aspectRatios: string[];
  note: string;
  sources: string[];
  checkedAt: string;
  retired: boolean;
};
export type ModelOption = {
  mediaProfile?: MediaProfile | null;
  description: string;
  creditPrice?: CreditPriceDTO | null;
  priceVersion?: number;
  billingUnit?: string;
  free?: boolean;
  id: string;
  name: string;
  connectionName?: string;
  provider?: string;
  modelId?: string;
  logoUrl?: string;
};
export type ModelOptions = {
  analysis: ModelOption[];
  image: ModelOption[];
  video: ModelOption[];
  catalog?: ModelOption[];
};
export type TokenPricing = {
  inputCnyPerMillion: number;
  outputCnyPerMillion: number;
  cacheHitHundredthsCnyPerMillion: number;
};
export type ModelSettingsInput = {
  resolutionEnabled?: Record<string, boolean> | null;
  mockBehavior?: { delaySeconds: number; failurePercent: number } | null;
  description: string;
  creditPrice: CreditPriceDTO | null;
  id: string;
  logoId: string;
  modelId: string;
  alias: string;
  category: 'analysis' | 'image' | 'video';
  enabled: boolean;
  contextWindow: number;
  maxOutputTokens: number;
  pricing: TokenPricing | null;
};
export type ConfiguredModel = ModelSettingsInput & {
  mediaProfile?: MediaProfile | null;
  name: string;
  defaultName?: string;
  provider: string;
  logoUrl: string;
  defaultLogoUrl: string;
  available: boolean;
  reason: string;
  referenced: boolean;
};
export type ModelConnection = {
  id: string;
  name: string;
  kind: 'official' | 'gateway' | 'builtin';
  protocol:
    | 'deepseek'
    | 'openai-chat'
    | 'volcengine-ark'
    | 'kling-media'
    | 'dashscope-media'
    | 'gemini'
    | 'openai-image'
    | 'mock-media';
  baseUrl: string;
  credentialConfigured: boolean;
  editable: boolean;
  reason: string;
  models: ConfiguredModel[];
};
export type ConnectionInput = Pick<
  ModelConnection,
  'id' | 'name' | 'kind' | 'protocol' | 'baseUrl'
> & {
  models: ModelSettingsInput[];
};
export type ModelConfiguration = {
  revision: number;
  editable: boolean;
  connections: ModelConnection[];
};
export type ProjectWorkflowState = {
  outline: TaskRun | null;
  extraction: TaskRun | null;
  visual: TaskRun | null;
  active: TaskRun | null;
  failed: TaskRun | null;
  cancelled: TaskRun | null;
  editable: boolean;
  label: string;
  status: string;
  reason: string;
  editReason: string;
  generatedCount: number;
  targetCount: number;
};
export type AssetInput = Pick<Asset, 'kind' | 'name' | 'description' | 'aliases' | 'character'>;
export type MediaTask = {
  id: string;
  scopeId: string;
  revision: number;
  modelId: string;
  operation: string;
  status: 'queued' | 'running' | 'unknown' | 'succeeded' | 'failed' | 'cancelled';
  providerTaskId: string | null;
  providerStatus: string | null;
  nextRunAt: string | null;
  errorCode: string | null;
  error: string | null;
  createdAt: string;
  updatedAt: string;
  billing: {
    status: 'pending' | 'known' | 'unknown';
    knownMicroCny: number;
    reservedMicroCny: number | null;
  };
  result: {
    files: {
      id: string;
      fileId?: string;
      thumbnailUrl?: string;
      contentType: string;
      contentUrl: string;
      sizeBytes: number;
      sha256: string;
      width: number;
      height: number;
      durationSeconds: number | null;
    }[];
  } | null;
};
export interface UploadedFile {
  spaceId: string;
  ownerId: string;
  creatorId: string;
  fileId: string;
  filename: string;
  contentType: string;
  sizeBytes: number;
  originalUrl: string;
  thumbnailUrl: string | null;
}

export interface UploadOptions {
  thumbnail?: boolean;
  thumbnailMaxWidth?: number;
  thumbnailMaxHeight?: number;
}

export interface CreateProjectInput {
  billingBasis?: string;
  fileId: string;
  title: string;
  settings?: ProjectSettings;
  styleId?: string;
  promptVersion?: string;
}

export interface CatalogAPI {
  uploadModelLogo(file: File): Promise<{
    logoId: string;
    logoUrl: string;
  }>;
  discoverModels(data: {
    revision: number;
    connectionId: string;
    baseUrl: string;
    protocol: string;
    apiKey?: string;
  }): Promise<{
    models: {
      modelId: string;
    }[];
  }>;
  getModelConfiguration(): Promise<ModelConfiguration>;
  saveModelConfiguration(data: {
    revision: number;
    connection: ConnectionInput;
    apiKey?: string;
  }): Promise<ModelConfiguration>;
  deleteModelConnection(
    id: string,
    data: {
      revision: number;
    },
  ): Promise<ModelConfiguration>;
  getStatus(): Promise<ServiceStatus>;
  listProjects(filter?: ListFilter): Promise<ProjectRow[]>;
  getProject(id: string): Promise<Project>;
  getSource(id: string): Promise<ScriptSource>;
  deleteProject(
    id: string,
    data: {
      revision: number;
    },
  ): Promise<{
    id: string;
    revision: number;
    deletedAt: string;
  }>;
  listModels(): Promise<ModelOptions>;
  listStyles(): Promise<{
    styles: VisualStyle[];
  }>;
  uploadFile(file: File, options?: UploadOptions, signal?: AbortSignal): Promise<UploadedFile>;
  saveAssetImage(
    id: string,
    target: VisualTarget,
    data: { revision: number; fileId: string },
    signal?: AbortSignal,
  ): Promise<Project>;
  getFile(fileId: string): Promise<UploadedFile>;
  createProject(input: CreateProjectInput): Promise<Project>;
  saveSettings(
    id: string,
    data: {
      revision: number;
      settings: ProjectSettings;
    },
  ): Promise<Project>;
  saveStyle(
    id: string,
    data: {
      revision: number;
      styleId: string;
      promptVersion: string;
    },
  ): Promise<Project>;
  createAsset(
    id: string,
    data: {
      revision: number;
      asset: AssetInput;
    },
  ): Promise<Project>;
  updateAsset(
    id: string,
    assetId: string,
    data: {
      revision: number;
      asset: AssetInput;
    },
  ): Promise<Project>;
  deleteAsset(
    id: string,
    assetId: string,
    data: {
      revision: number;
    },
  ): Promise<Project>;
  createLook(
    id: string,
    assetId: string,
    data: {
      revision: number;
      look: {
        name: string;
        description: string;
      };
    },
  ): Promise<Project>;
  updateLook(
    id: string,
    assetId: string,
    lookId: string,
    data: {
      revision: number;
      look: {
        name: string;
        description: string;
      };
    },
  ): Promise<Project>;
  deleteLook(
    id: string,
    assetId: string,
    lookId: string,
    data: {
      revision: number;
    },
  ): Promise<Project>;
  saveVisualPrompt(
    id: string,
    target: VisualTarget,
    data: { revision: number; text: string },
  ): Promise<Project>;
  startExtraction(
    id: string,
    data: {
      revision: number;
      billingBasis?: string;
    },
  ): Promise<Project>;
  startVisualPrompts(
    id: string,
    data: {
      revision: number;
      billingBasis?: string;
    },
  ): Promise<Project>;
  startSingleVisualPrompts(
    id: string,
    target: VisualTarget,
    data: {
      revision: number;
      billingBasis?: string;
    },
  ): Promise<Project>;
  cancelTask(
    id: string,
    data: {
      revision: number;
    },
  ): Promise<Project>;
}
