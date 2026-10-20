import type { FormEvent } from 'react';
import type { CatalogText } from '../../contexts/useCatalogText';
import type {
  ConnectionInput,
  ModelConfiguration,
  ModelConnection,
  ModelSettingsInput,
  ServiceStatus,
  TokenPricing,
} from '../../api/types';
import type { DirectoryItem } from './model-config-data';
type Category = ModelSettingsInput['category'];
export type ConnectionListView = {
  c: CatalogText;
  status: ServiceStatus | null;
  registry: ModelConfiguration | null;
  category: Category;
  source: string;
  search: string;
  loading: boolean;
  error: string;
  busy: boolean;
  deleting: ModelConnection | null;
  editable: boolean;
  unsupported: boolean | null;
  selectCategory: (value: Category) => void;
  selectSource: (value: string) => void;
  searchConnections: (value: string) => void;
  refresh: () => Promise<ModelConfiguration>;
  edit: (connection?: ModelConnection, trigger?: HTMLButtonElement) => void;
  requestDelete: (connection: ModelConnection) => void;
  cancelDelete: () => void;
  remove: () => Promise<void>;
};
type ConnectionFieldsView = {
  c: CatalogText;
  loading: boolean;
  busy: boolean;
  draft: Readonly<ConnectionInput>;
  original: ModelConnection | null;
  apiKey: string;
  showKey: boolean;
  formError: string;
  conflict: boolean;
  expanded: readonly number[];
  editCategory: string;
  updateCredential: (value: string) => void;
  toggleCredentialVisibility: () => void;
  selectEditorCategory: (value: string) => void;
  toggleModelDetails: (index: number) => void;
  patch: (index: number, change: Partial<ModelSettingsInput>) => void;
  pricing: (index: number, key: keyof TokenPricing, value: string) => void;
  refreshDraft: () => Promise<void>;
};
export type ConnectionEditorView = ConnectionFieldsView & {
  open: boolean;
  activity: string;
  editable: boolean;
  official: boolean;
  returnFocusId?: string;
  close: () => void;
  clearFormError: () => void;
  save: (event: FormEvent<HTMLFormElement>) => Promise<void>;
  upload: (index: number, file?: File) => Promise<void>;
};
export type GatewayEditorView = ConnectionFieldsView & {
  directory: readonly DirectoryItem[] | null;
  directoryError: string;
  directorySearch: string;
  updateConnection: (
    change: Partial<Pick<ConnectionInput, 'name' | 'protocol' | 'baseUrl'>>,
  ) => void;
  addModel: (category: Category) => void;
  removeModel: (index: number) => void;
  closeDiscovery: () => void;
  searchDiscovery: (value: string) => void;
  updateDiscoveredModel: (
    id: string,
    change: Partial<Pick<DirectoryItem, 'selected' | 'category'>>,
  ) => void;
  discover: () => Promise<void>;
  importSelected: () => void;
};
export type ModelConfigurationViews = {
  listing: ConnectionListView;
  editor: ConnectionEditorView;
  gateway: GatewayEditorView;
};
