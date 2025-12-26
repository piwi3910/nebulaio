import axios from 'axios';
import { useAuthStore } from '../stores/auth';

const API_BASE_URL = '/api/v1';

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

  // Buckets
  listBuckets: () => apiClient.get('/admin/buckets'),
  createBucket: (data: { name: string; region?: string; storage_class?: string }) =>
    apiClient.post('/admin/buckets', data),
  getBucket: (name: string) => apiClient.get(`/admin/buckets/${name}`),
  deleteBucket: (name: string) => apiClient.delete(`/admin/buckets/${name}`),

  // Cluster
  getClusterStatus: () => apiClient.get('/admin/cluster/status'),
  listNodes: () => apiClient.get('/admin/cluster/nodes'),

  // Storage
  getStorageInfo: () => apiClient.get('/admin/storage/info'),
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
    apiClient.get(`/console/buckets/${bucket}/objects/${key}`),
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
  deleteObject: (bucket: string, key: string) =>
    apiClient.delete(`/console/buckets/${bucket}/objects/${key}`),
};
