import axios from 'axios';
import { useAuthStore } from '../stores/auth';

const API_BASE_URL = '/api/v1';

// Types for bucket settings
export interface LifecycleRule {
  id: string;
  prefix: string;
  enabled: boolean;
  expiration_days?: number;
  noncurrent_expiration_days?: number;
  transitions?: {
    days: number;
    storage_class: string;
  }[];
}

export interface CorsRule {
  allowed_origins: string[];
  allowed_methods: string[];
  allowed_headers: string[];
  expose_headers?: string[];
  max_age_seconds?: number;
}

export interface AuditLogEntry {
  id: string;
  timestamp: string;
  event_type: string;
  user_id: string;
  username: string;
  bucket?: string;
  object_key?: string;
  source_ip: string;
  user_agent: string;
  request_id: string;
  status_code: number;
  details: Record<string, unknown>;
}

export interface Policy {
  name: string;
  description: string;
  document: string;
  created_at: string;
  updated_at: string;
}

// Security Feature Types
export interface Anomaly {
  id: string;
  type: string;
  severity: 'LOW' | 'MEDIUM' | 'HIGH' | 'CRITICAL';
  user_id: string;
  description: string;
  detected_at: string;
  acknowledged: boolean;
  acknowledged_by?: string;
  acknowledged_at?: string;
  resolution?: string;
  details: Record<string, unknown>;
}

export interface AnalyticsStats {
  total_events: number;
  anomalies_detected: number;
  users_monitored: number;
  baselines_created: number;
  events_last_24h: number;
  anomalies_last_24h: number;
  anomalies_by_severity?: Record<string, number>;
  anomalies_by_type?: Record<string, number>;
}

export interface EncryptionKey {
  id: string;
  type: 'MASTER' | 'DATA' | 'BUCKET' | 'OBJECT' | 'CUSTOMER';
  algorithm: string;
  alias?: string;
  version: number;
  status: 'ACTIVE' | 'ROTATING' | 'PENDING_DELETION' | 'DELETED';
  created_at: string;
  rotated_at?: string;
  expires_at?: string;
  metadata?: Record<string, string>;
}

export interface KeyRotationEvent {
  id: string;
  key_id: string;
  old_version: number;
  new_version: number;
  trigger: 'scheduled' | 'manual' | 'policy';
  initiated_by?: string;
  started_at: string;
  completed_at?: string;
  status: 'IN_PROGRESS' | 'COMPLETED' | 'FAILED';
  error?: string;
}

export interface Certificate {
  id: string;
  type: 'SERVER' | 'CLIENT' | 'PEER';
  common_name: string;
  dns_names?: string[];
  ip_addresses?: string[];
  serial_number: string;
  not_before: string;
  not_after: string;
  revoked: boolean;
  revoked_at?: string;
  revocation_reason?: string;
}

export interface TracingStats {
  enabled: boolean;
  service_name: string;
  sampling_rate: number;
  exporter_type: string;
  spans_exported: number;
  spans_dropped: number;
  active_spans: number;
  propagator: string;
}

// Tiering Policy Types
export interface TieringTrigger {
  type: 'age' | 'access' | 'capacity';
  age_days?: number;
  access_count?: number;
  access_days?: number;
  capacity_percent?: number;
}

export interface TieringAction {
  type: 'transition' | 'delete';
  target_tier?: string;
  notify_url?: string;
}

export interface TieringSchedule {
  maintenance_windows?: string[];
  blackout_windows?: string[];
}

export interface TieringAdvancedOptions {
  rate_limit?: number;
  anti_thrash_hours?: number;
  distributed_execution?: boolean;
}

export interface TieringPolicy {
  id: string;
  name: string;
  description: string;
  type: 'scheduled' | 'realtime' | 'threshold' | 's3_lifecycle';
  scope: 'global' | 'bucket' | 'prefix';
  bucket_pattern?: string;
  prefix_pattern?: string;
  enabled: boolean;
  triggers: TieringTrigger[];
  actions: TieringAction[];
  schedule?: TieringSchedule;
  cron_expression?: string;
  advanced_options?: TieringAdvancedOptions;
  created_at: string;
  updated_at: string;
  last_run?: string;
  stats?: {
    objects_affected: number;
    transitions_performed: number;
    last_execution_time?: string;
    success_count: number;
    failure_count: number;
  };
}

export const apiClient = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
});

// Request interceptor to add auth token
apiClient.interceptors.request.use((config) => {
  const token = useAuthStore.getState().accessToken;
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

// Response interceptor for token refresh
apiClient.interceptors.response.use(
  (response) => response,
  async (error) => {
    const originalRequest = error.config;

    if (error.response?.status === 401 && !originalRequest._retry) {
      originalRequest._retry = true;

      const refreshToken = useAuthStore.getState().refreshToken;
      if (refreshToken) {
        try {
          const response = await axios.post(`${API_BASE_URL}/admin/auth/refresh`, {
            refresh_token: refreshToken,
          });

          const { access_token, refresh_token } = response.data;
          useAuthStore.getState().setTokens(access_token, refresh_token);

          originalRequest.headers.Authorization = `Bearer ${access_token}`;
          return apiClient(originalRequest);
        } catch {
          useAuthStore.getState().logout();
          window.location.href = '/login';
        }
      } else {
        useAuthStore.getState().logout();
        window.location.href = '/login';
      }
    }

    return Promise.reject(error);
  }
);

// Admin API
export const adminApi = {
  // Auth
  login: (username: string, password: string) =>
    apiClient.post('/admin/auth/login', { username, password }),

  refresh: (refreshToken: string) =>
    apiClient.post('/admin/auth/refresh', { refresh_token: refreshToken }),

  // Users
  listUsers: () => apiClient.get('/admin/users'),
  createUser: (data: { username: string; password: string; email: string; role: string }) =>
    apiClient.post('/admin/users', data),
  getUser: (id: string) => apiClient.get(`/admin/users/${id}`),
  updateUser: (id: string, data: Partial<{ email: string; role: string; enabled: boolean }>) =>
    apiClient.put(`/admin/users/${id}`, data),
  deleteUser: (id: string) => apiClient.delete(`/admin/users/${id}`),
  updatePassword: (id: string, password: string) =>
    apiClient.put(`/admin/users/${id}/password`, { password }),

  // Access Keys
  listAccessKeys: (userId: string) => apiClient.get(`/admin/users/${userId}/keys`),
  createAccessKey: (userId: string, description: string) =>
    apiClient.post(`/admin/users/${userId}/keys`, { description }),
  deleteAccessKey: (accessKeyId: string) => apiClient.delete(`/admin/keys/${accessKeyId}`),

  // Policies
  listPolicies: () => apiClient.get('/admin/policies'),
  createPolicy: (data: { name: string; description: string; document: string }) =>
    apiClient.post('/admin/policies', data),
  getPolicy: (name: string) => apiClient.get(`/admin/policies/${name}`),
  updatePolicy: (name: string, data: { description: string; document: string }) =>
    apiClient.put(`/admin/policies/${name}`, data),
  deletePolicy: (name: string) => apiClient.delete(`/admin/policies/${name}`),
  attachPolicyToUser: (policyName: string, userId: string) =>
    apiClient.post(`/admin/policies/${policyName}/attach`, { user_id: userId }),
  detachPolicyFromUser: (policyName: string, userId: string) =>
    apiClient.post(`/admin/policies/${policyName}/detach`, { user_id: userId }),

  // Buckets
  listBuckets: () => apiClient.get('/admin/buckets'),
  createBucket: (data: { name: string; region?: string; storage_class?: string }) =>
    apiClient.post('/admin/buckets', data),
  getBucket: (name: string) => apiClient.get(`/admin/buckets/${name}`),
  deleteBucket: (name: string) => apiClient.delete(`/admin/buckets/${name}`),

  // Bucket Settings
  getBucketVersioning: (name: string) => apiClient.get(`/admin/buckets/${name}/versioning`),
  setBucketVersioning: (name: string, enabled: boolean) =>
    apiClient.put(`/admin/buckets/${name}/versioning`, { enabled }),
  getBucketLifecycle: (name: string) => apiClient.get(`/admin/buckets/${name}/lifecycle`),
  setBucketLifecycle: (name: string, rules: LifecycleRule[]) =>
    apiClient.put(`/admin/buckets/${name}/lifecycle`, { rules }),
  deleteBucketLifecycle: (name: string) => apiClient.delete(`/admin/buckets/${name}/lifecycle`),
  getBucketCors: (name: string) => apiClient.get(`/admin/buckets/${name}/cors`),
  setBucketCors: (name: string, rules: CorsRule[]) =>
    apiClient.put(`/admin/buckets/${name}/cors`, { rules }),
  deleteBucketCors: (name: string) => apiClient.delete(`/admin/buckets/${name}/cors`),
  getBucketPolicy: (name: string) => apiClient.get(`/admin/buckets/${name}/policy`),
  setBucketPolicy: (name: string, policy: string) =>
    apiClient.put(`/admin/buckets/${name}/policy`, { policy }),
  deleteBucketPolicy: (name: string) => apiClient.delete(`/admin/buckets/${name}/policy`),
  getBucketTags: (name: string) => apiClient.get(`/admin/buckets/${name}/tags`),
  setBucketTags: (name: string, tags: Record<string, string>) =>
    apiClient.put(`/admin/buckets/${name}/tags`, { tags }),
  deleteBucketTags: (name: string) => apiClient.delete(`/admin/buckets/${name}/tags`),

  // Cluster
  getClusterStatus: () => apiClient.get('/admin/cluster/status'),
  listNodes: () => apiClient.get('/admin/cluster/nodes'),
  getNodeMetrics: (nodeId: string) => apiClient.get(`/admin/cluster/nodes/${nodeId}/metrics`),
  getRaftState: () => apiClient.get('/admin/cluster/raft'),

  // Storage
  getStorageInfo: () => apiClient.get('/admin/storage/info'),
  getStorageMetrics: () => apiClient.get('/admin/storage/metrics'),

  // Audit Logs
  listAuditLogs: (params?: {
    bucket?: string;
    user_id?: string;
    event_type?: string;
    start_date?: string;
    end_date?: string;
    page?: number;
    page_size?: number;
  }) => apiClient.get('/admin/audit-logs', { params }),

  // Server Configuration
  getConfig: () => apiClient.get('/admin/config'),
  updateConfig: <T extends object>(config: T) => apiClient.put('/admin/config', config),

  // AI/ML Features
  getAIMLMetrics: () => apiClient.get('/admin/aiml/metrics'),
  getAIMLFeatureMetrics: (feature: string) => apiClient.get(`/admin/aiml/metrics/${feature}`),
  getAIMLStatus: () => apiClient.get('/admin/aiml/status'),

  // Security - Access Analytics
  listAnomalies: (params?: { severity?: string; acknowledged?: boolean; limit?: number }) =>
    apiClient.get('/analytics/anomalies', { params }),
  getAnalyticsStats: () => apiClient.get('/analytics/stats'),
  acknowledgeAnomaly: (id: string, data: { resolution?: string }) =>
    apiClient.post(`/analytics/anomalies/${id}/acknowledge`, data),
  getUserBaseline: (userId: string) => apiClient.get(`/analytics/baselines/${userId}`),

  // Security - Key Rotation
  listKeys: (params?: { type?: string }) => apiClient.get('/keys', { params }),
  createKey: (data: { type: string; algorithm: string; alias?: string; metadata?: Record<string, string> }) =>
    apiClient.post('/keys', data),
  getKey: (id: string) => apiClient.get(`/keys/${id}`),
  rotateKey: (id: string, data?: { trigger?: string }) =>
    apiClient.post(`/keys/${id}/rotate`, data),
  getKeyRotations: (id: string) => apiClient.get(`/keys/${id}/rotations`),
  startReencryption: (id: string, data: { old_version: number; new_version: number; buckets?: string[]; concurrency?: number }) =>
    apiClient.post(`/keys/${id}/reencrypt`, data),

  // Security - mTLS
  initCA: (data: { common_name: string }) => apiClient.post('/mtls/ca/init', data),
  getCA: () => apiClient.get('/mtls/ca/certificate'),
  listCertificates: () => apiClient.get('/mtls/certificates'),
  issueCertificate: (data: { type: string; common_name: string; dns_names?: string[]; ip_addresses?: string[] }) =>
    apiClient.post('/mtls/certificates', data),
  getCertificate: (id: string) => apiClient.get(`/mtls/certificates/${id}`),
  revokeCertificate: (id: string, data: { reason: string }) =>
    apiClient.post(`/mtls/certificates/${id}/revoke`, data),
  getCRL: () => apiClient.get('/mtls/crl'),

  // Security - Distributed Tracing
  getTracingConfig: () => apiClient.get('/tracing/config'),
  updateTracingConfig: (data: { enabled?: boolean; sampling?: { rate: number }; exporter?: { type: string; endpoint: string }; propagator?: string }) =>
    apiClient.put('/tracing/config', data),
  getTracingStats: () => apiClient.get('/tracing/stats'),

  // Security - Feature Configuration
  getSecurityConfig: () => apiClient.get('/admin/security/config'),
  updateSecurityConfig: (data: { analytics_enabled?: boolean; key_rotation_enabled?: boolean; mtls_enabled?: boolean; tracing_enabled?: boolean }) =>
    apiClient.put('/admin/security/config', data),

  // Tiering Policies
  listTieringPolicies: (params?: { type?: string; scope?: string; enabled?: boolean }) =>
    apiClient.get('/admin/tiering-policies', { params }),
  createTieringPolicy: (data: Omit<TieringPolicy, 'id' | 'created_at' | 'updated_at' | 'last_run' | 'stats'>) =>
    apiClient.post('/admin/tiering-policies', data),
  getTieringPolicy: (id: string) => apiClient.get(`/admin/tiering-policies/${id}`),
  updateTieringPolicy: (id: string, data: Partial<Omit<TieringPolicy, 'id' | 'created_at' | 'updated_at' | 'last_run' | 'stats'>>) =>
    apiClient.put(`/admin/tiering-policies/${id}`, data),
  deleteTieringPolicy: (id: string) => apiClient.delete(`/admin/tiering-policies/${id}`),
  enableTieringPolicy: (id: string) => apiClient.post(`/admin/tiering-policies/${id}/enable`),
  disableTieringPolicy: (id: string) => apiClient.post(`/admin/tiering-policies/${id}/disable`),
  getTieringPolicyStats: (id: string) => apiClient.get(`/admin/tiering-policies/${id}/stats`),
};

// Console API (user-facing)
export const consoleApi = {
  // Profile
  getCurrentUser: () => apiClient.get('/console/me'),
  updatePassword: (currentPassword: string, newPassword: string) =>
    apiClient.put('/console/me/password', {
      current_password: currentPassword,
      new_password: newPassword,
    }),

  // Access Keys
  listMyAccessKeys: () => apiClient.get('/console/me/keys'),
  createMyAccessKey: (description: string) =>
    apiClient.post('/console/me/keys', { description }),
  deleteMyAccessKey: (accessKeyId: string) =>
    apiClient.delete(`/console/me/keys/${accessKeyId}`),

  // Buckets
  listMyBuckets: () => apiClient.get('/console/buckets'),
  listBucketObjects: (bucket: string, params?: { prefix?: string; delimiter?: string; max_keys?: number; page_token?: string }) =>
    apiClient.get(`/console/buckets/${bucket}/objects`, { params }),
  getObjectInfo: (bucket: string, key: string) =>
    apiClient.get(`/console/buckets/${bucket}/objects/${encodeURIComponent(key)}/info`),
  uploadObject: (bucket: string, file: File, path?: string) => {
    const formData = new FormData();
    formData.append('file', file);
    if (path) {
      formData.append('path', path);
    }
    return apiClient.post(`/console/buckets/${bucket}/objects`, formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    });
  },
  uploadObjectWithProgress: (
    bucket: string,
    file: File,
    path: string | undefined,
    onProgress: (progress: number) => void
  ) => {
    const formData = new FormData();
    formData.append('file', file);
    if (path) {
      formData.append('path', path);
    }
    return apiClient.post(`/console/buckets/${bucket}/objects`, formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
      onUploadProgress: (progressEvent) => {
        if (progressEvent.total) {
          const percent = Math.round((progressEvent.loaded * 100) / progressEvent.total);
          onProgress(percent);
        }
      },
    });
  },
  createFolder: (bucket: string, folderPath: string) => {
    // Create an empty object with trailing slash to represent a folder
    const formData = new FormData();
    const emptyBlob = new Blob([''], { type: 'application/x-directory' });
    const normalizedPath = folderPath.endsWith('/') ? folderPath : `${folderPath}/`;
    formData.append('file', emptyBlob, '');
    formData.append('key', normalizedPath);
    return apiClient.post(`/console/buckets/${bucket}/objects`, formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    });
  },
  deleteObject: (bucket: string, key: string) =>
    apiClient.delete(`/console/buckets/${bucket}/objects/${encodeURIComponent(key)}`),
  getPresignedUrl: (bucket: string, key: string, expiresIn?: number) =>
    apiClient.get(`/console/buckets/${bucket}/objects/${encodeURIComponent(key)}/presigned`, {
      params: { expires_in: expiresIn || 3600 },
    }),
  getPresignedDownloadUrl: (bucket: string, key: string, expiresIn?: number) =>
    apiClient.get(`/console/buckets/${bucket}/objects/${encodeURIComponent(key)}/download-url`, {
      params: { expires_in: expiresIn || 3600 },
    }),
  getObjectContent: (bucket: string, key: string) =>
    apiClient.get(`/console/buckets/${bucket}/objects/${encodeURIComponent(key)}/content`, {
      responseType: 'blob',
    }),

  // Bucket Settings (user-facing)
  getBucketSettings: (bucket: string) => apiClient.get(`/console/buckets/${bucket}/settings`),
};
