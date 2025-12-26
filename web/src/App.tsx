import { Routes, Route, Navigate } from 'react-router-dom';
import { useAuthStore } from './stores/auth';
import { LoginPage } from './pages/LoginPage';
import { DashboardLayout } from './components/DashboardLayout';
import { DashboardPage } from './pages/DashboardPage';
import { BucketsPage } from './pages/BucketsPage';
import { BucketBrowserPage } from './pages/BucketBrowserPage';
import { UsersPage } from './pages/UsersPage';
import { AccessKeysPage } from './pages/AccessKeysPage';
import { ClusterPage } from './pages/ClusterPage';
import { SettingsPage } from './pages/SettingsPage';

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

        {/* Admin routes */}
        <Route
          path="dashboard"
          element={
            <AdminRoute>
              <DashboardPage />
            </AdminRoute>
          }
        />
        <Route
          path="users"
          element={
            <AdminRoute>
              <UsersPage />
            </AdminRoute>
          }
        />
        <Route
          path="cluster"
          element={
            <AdminRoute>
              <ClusterPage />
            </AdminRoute>
          }
        />

        {/* User routes */}
        <Route path="buckets" element={<BucketsPage />} />
        <Route path="buckets/:bucketName/*" element={<BucketBrowserPage />} />
        <Route path="access-keys" element={<AccessKeysPage />} />
        <Route path="settings" element={<SettingsPage />} />
      </Route>

      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
