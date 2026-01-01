import { Suspense, lazy } from 'react';
import { Routes, Route, Navigate } from 'react-router-dom';
import { Center, Loader, Text, Stack } from '@mantine/core';
import { useAuthStore } from './stores/auth';
import { ErrorBoundary } from './components/ErrorBoundary';

// Lazy load all pages for code splitting
const LoginPage = lazy(() => import('./pages/LoginPage').then(m => ({ default: m.LoginPage })));
const DashboardLayout = lazy(() => import('./components/DashboardLayout').then(m => ({ default: m.DashboardLayout })));
const DashboardPage = lazy(() => import('./pages/DashboardPage').then(m => ({ default: m.DashboardPage })));
const BucketsPage = lazy(() => import('./pages/BucketsPage').then(m => ({ default: m.BucketsPage })));
const BucketBrowserPage = lazy(() => import('./pages/BucketBrowserPage').then(m => ({ default: m.BucketBrowserPage })));
const BucketSettingsPage = lazy(() => import('./pages/BucketSettingsPage').then(m => ({ default: m.BucketSettingsPage })));
const UsersPage = lazy(() => import('./pages/UsersPage').then(m => ({ default: m.UsersPage })));
const AccessKeysPage = lazy(() => import('./pages/AccessKeysPage').then(m => ({ default: m.AccessKeysPage })));
const UserAccessKeysPage = lazy(() => import('./pages/UserAccessKeysPage').then(m => ({ default: m.UserAccessKeysPage })));
const ClusterPage = lazy(() => import('./pages/ClusterPage').then(m => ({ default: m.ClusterPage })));
const SettingsPage = lazy(() => import('./pages/SettingsPage').then(m => ({ default: m.SettingsPage })));
const PoliciesPage = lazy(() => import('./pages/PoliciesPage').then(m => ({ default: m.PoliciesPage })));
const AuditLogsPage = lazy(() => import('./pages/AuditLogsPage').then(m => ({ default: m.AuditLogsPage })));
const AIMLFeaturesPage = lazy(() => import('./pages/AIMLFeaturesPage').then(m => ({ default: m.AIMLFeaturesPage })));
const SecurityFeaturesPage = lazy(() => import('./pages/SecurityFeaturesPage').then(m => ({ default: m.SecurityFeaturesPage })));
const TieringPoliciesPage = lazy(() => import('./pages/TieringPoliciesPage').then(m => ({ default: m.TieringPoliciesPage })));
const PredictiveAnalyticsPage = lazy(() => import('./pages/PredictiveAnalyticsPage').then(m => ({ default: m.PredictiveAnalyticsPage })));
const ApiExplorerPage = lazy(() => import('./pages/ApiExplorerPage').then(m => ({ default: m.ApiExplorerPage })));

// Loading fallback component
function PageLoader() {
  return (
    <Center h="100vh">
      <Stack align="center" gap="md">
        <Loader size="lg" />
        <Text c="dimmed" size="sm">Loading...</Text>
      </Stack>
    </Center>
  );
}

// Content loader for nested routes
function ContentLoader() {
  return (
    <Center h="50vh">
      <Loader size="md" />
    </Center>
  );
}

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated);

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }

  return <>{children}</>;
}

function AdminRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated, user } = useAuthStore();

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }

  if (user?.role !== 'superadmin' && user?.role !== 'admin') {
    return <Navigate to="/buckets" replace />;
  }

  return <>{children}</>;
}

export default function App() {
  return (
    <ErrorBoundary>
      <Suspense fallback={<PageLoader />}>
        <Routes>
        <Route path="/login" element={<LoginPage />} />

        <Route
          path="/"
          element={
            <ProtectedRoute>
              <DashboardLayout />
            </ProtectedRoute>
          }
        >
          <Route index element={<Navigate to="/dashboard" replace />} />

          {/* Admin routes - wrapped in Suspense for nested lazy loading */}
          <Route
            path="dashboard"
            element={
              <AdminRoute>
                <Suspense fallback={<ContentLoader />}>
                  <DashboardPage />
                </Suspense>
              </AdminRoute>
            }
          />
          <Route
            path="users"
            element={
              <AdminRoute>
                <Suspense fallback={<ContentLoader />}>
                  <UsersPage />
                </Suspense>
              </AdminRoute>
            }
          />
          <Route
            path="users/:userId/access-keys"
            element={
              <AdminRoute>
                <Suspense fallback={<ContentLoader />}>
                  <UserAccessKeysPage />
                </Suspense>
              </AdminRoute>
            }
          />
          <Route
            path="policies"
            element={
              <AdminRoute>
                <Suspense fallback={<ContentLoader />}>
                  <PoliciesPage />
                </Suspense>
              </AdminRoute>
            }
          />
          <Route
            path="cluster"
            element={
              <AdminRoute>
                <Suspense fallback={<ContentLoader />}>
                  <ClusterPage />
                </Suspense>
              </AdminRoute>
            }
          />
          <Route
            path="audit-logs"
            element={
              <AdminRoute>
                <Suspense fallback={<ContentLoader />}>
                  <AuditLogsPage />
                </Suspense>
              </AdminRoute>
            }
          />
          <Route
            path="ai-ml-features"
            element={
              <AdminRoute>
                <Suspense fallback={<ContentLoader />}>
                  <AIMLFeaturesPage />
                </Suspense>
              </AdminRoute>
            }
          />
          <Route
            path="security"
            element={
              <AdminRoute>
                <Suspense fallback={<ContentLoader />}>
                  <SecurityFeaturesPage />
                </Suspense>
              </AdminRoute>
            }
          />
          <Route
            path="tiering-policies"
            element={
              <AdminRoute>
                <Suspense fallback={<ContentLoader />}>
                  <TieringPoliciesPage />
                </Suspense>
              </AdminRoute>
            }
          />
          <Route
            path="predictive-analytics"
            element={
              <AdminRoute>
                <Suspense fallback={<ContentLoader />}>
                  <PredictiveAnalyticsPage />
                </Suspense>
              </AdminRoute>
            }
          />
          <Route
            path="api-explorer"
            element={
              <AdminRoute>
                <Suspense fallback={<ContentLoader />}>
                  <ApiExplorerPage />
                </Suspense>
              </AdminRoute>
            }
          />

          {/* User routes */}
          <Route
            path="buckets"
            element={
              <Suspense fallback={<ContentLoader />}>
                <BucketsPage />
              </Suspense>
            }
          />
          <Route
            path="buckets/:bucketName/settings"
            element={
              <Suspense fallback={<ContentLoader />}>
                <BucketSettingsPage />
              </Suspense>
            }
          />
          <Route
            path="buckets/:bucketName/*"
            element={
              <Suspense fallback={<ContentLoader />}>
                <BucketBrowserPage />
              </Suspense>
            }
          />
          <Route
            path="access-keys"
            element={
              <Suspense fallback={<ContentLoader />}>
                <AccessKeysPage />
              </Suspense>
            }
          />
          <Route
            path="settings"
            element={
              <Suspense fallback={<ContentLoader />}>
                <SettingsPage />
              </Suspense>
            }
          />
        </Route>

        <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </Suspense>
    </ErrorBoundary>
  );
}
