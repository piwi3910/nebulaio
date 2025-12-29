import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, cleanup } from '../test/utils';
import { SecurityFeaturesPage } from './SecurityFeaturesPage';
import { http, HttpResponse } from 'msw';
import { server } from '../test/mocks/server';

// Mock data
const mockAnalyticsStats = {
  total_events: 15000,
  users_monitored: 25,
  anomalies_detected: 3,
  events_last_24h: 1200,
  anomalies_by_severity: {
    CRITICAL: 1,
    HIGH: 1,
    MEDIUM: 1,
    LOW: 0,
  },
};

const mockAnomalies = {
  anomalies: [
    {
      id: 'anomaly-1',
      type: 'unusual_access_pattern',
      severity: 'HIGH',
      user_id: 'user-1',
      detected_at: '2024-01-15T10:00:00Z',
      description: 'Unusual access pattern detected',
      acknowledged: false,
    },
    {
      id: 'anomaly-2',
      type: 'failed_login_spike',
      severity: 'CRITICAL',
      user_id: 'user-2',
      detected_at: '2024-01-15T09:00:00Z',
      description: 'Multiple failed login attempts',
      acknowledged: true,
    },
  ],
};

const mockEncryptionKeys = {
  keys: [
    {
      id: 'key-1',
      alias: 'master-key',
      type: 'MASTER',
      algorithm: 'AES-256-GCM',
      version: 3,
      status: 'ACTIVE',
      rotated_at: '2024-01-10T00:00:00Z',
      expires_at: '2025-01-10T00:00:00Z',
    },
    {
      id: 'key-2',
      alias: 'data-key',
      type: 'DATA',
      algorithm: 'AES-256-GCM',
      version: 1,
      status: 'ACTIVE',
      rotated_at: null,
      expires_at: null,
    },
  ],
};

const mockCertificates = {
  certificates: [
    {
      id: 'cert-1',
      common_name: 'nebulaio-server',
      type: 'SERVER',
      serial_number: 'abc123def456789012345678',
      not_before: '2024-01-01T00:00:00Z',
      not_after: '2025-01-01T00:00:00Z',
      revoked: false,
    },
    {
      id: 'cert-2',
      common_name: 'nebulaio-ca',
      type: 'CA',
      serial_number: 'def456abc123789012345678',
      not_before: '2023-01-01T00:00:00Z',
      not_after: '2033-01-01T00:00:00Z',
      revoked: false,
    },
  ],
};

const mockTracingStats = {
  sampling_rate: 0.1,
  active_spans: 42,
  spans_exported: 150000,
  exporter_type: 'otlp',
};

const mockSecurityConfig = {
  access_analytics: {
    enabled: true,
    baseline_window: '24h',
    anomaly_threshold: 3.0,
    sampling_rate: 1.0,
  },
  key_rotation: {
    enabled: true,
    check_interval: '1h',
  },
  mtls: {
    enabled: false,
    require_client_cert: true,
  },
  tracing: {
    enabled: true,
    sampling_rate: 0.1,
    exporter_type: 'otlp',
    endpoint: 'http://otel-collector:4317',
  },
};

describe('SecurityFeaturesPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    server.resetHandlers();
    // Set up default handlers for security endpoints
    server.use(
      http.get('/api/v1/analytics/stats', () => {
        return HttpResponse.json(mockAnalyticsStats);
      }),
      http.get('/api/v1/analytics/anomalies', () => {
        return HttpResponse.json(mockAnomalies);
      }),
      http.get('/api/v1/keys', () => {
        return HttpResponse.json(mockEncryptionKeys);
      }),
      http.get('/api/v1/mtls/certificates', () => {
        return HttpResponse.json(mockCertificates);
      }),
      http.get('/api/v1/tracing/stats', () => {
        return HttpResponse.json(mockTracingStats);
      }),
      http.get('/api/v1/admin/security/config', () => {
        return HttpResponse.json(mockSecurityConfig);
      }),
      http.put('/api/v1/admin/security/config', () => {
        return HttpResponse.json({ status: 'ok' });
      }),
      http.post('/api/v1/analytics/anomalies/:id/acknowledge', () => {
        return HttpResponse.json({ status: 'ok' });
      }),
      http.post('/api/v1/keys/:id/rotate', () => {
        return HttpResponse.json({ status: 'ok' });
      }),
      http.post('/api/v1/mtls/certificates/:id/revoke', () => {
        return HttpResponse.json({ status: 'ok' });
      })
    );
  });

  afterEach(() => {
    cleanup();
  });

  it('renders the page title', () => {
    render(<SecurityFeaturesPage />);
    expect(screen.getByRole('heading', { name: /security features/i })).toBeInTheDocument();
  });

  it('renders the page subtitle', () => {
    render(<SecurityFeaturesPage />);
    expect(screen.getByText(/monitor and manage security features/i)).toBeInTheDocument();
  });

  it('renders all tab buttons', () => {
    render(<SecurityFeaturesPage />);
    expect(screen.getByRole('tab', { name: /access analytics/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /rate limiting/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /key rotation/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /mtls certificates/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /distributed tracing/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /configuration/i })).toBeInTheDocument();
  });

  it('shows Access Analytics tab by default', () => {
    render(<SecurityFeaturesPage />);
    const analyticsTab = screen.getByRole('tab', { name: /access analytics/i });
    expect(analyticsTab).toHaveAttribute('aria-selected', 'true');
  });

  describe('Access Analytics Tab', () => {
    it('displays analytics stats cards', async () => {
      render(<SecurityFeaturesPage />);

      await waitFor(() => {
        expect(screen.getByText('Total Events')).toBeInTheDocument();
        expect(screen.getByText('Users Monitored')).toBeInTheDocument();
        expect(screen.getByText('Anomalies')).toBeInTheDocument();
        expect(screen.getByText('Events (24h)')).toBeInTheDocument();
      });
    });

    it('displays anomalies by severity section', async () => {
      render(<SecurityFeaturesPage />);

      await waitFor(() => {
        expect(screen.getByText('Anomalies by Severity')).toBeInTheDocument();
        // Severity badges appear in both the summary and table, so use getAllByText
        expect(screen.getAllByText('CRITICAL').length).toBeGreaterThan(0);
        expect(screen.getAllByText('HIGH').length).toBeGreaterThan(0);
      });
    });

    it('displays recent anomalies section', async () => {
      render(<SecurityFeaturesPage />);

      await waitFor(() => {
        expect(screen.getByText('Recent Anomalies')).toBeInTheDocument();
      });
    });

    it('shows empty state when no anomalies', async () => {
      server.use(
        http.get('/api/v1/analytics/anomalies', () => {
          return HttpResponse.json({ anomalies: [] });
        })
      );

      render(<SecurityFeaturesPage />);

      await waitFor(() => {
        expect(screen.getByText('No anomalies detected')).toBeInTheDocument();
      });
    });

    it('shows loading skeleton while fetching data', () => {
      server.use(
        http.get('/api/v1/analytics/stats', async () => {
          await new Promise(resolve => setTimeout(resolve, 200));
          return HttpResponse.json(mockAnalyticsStats);
        })
      );

      render(<SecurityFeaturesPage />);
      expect(document.querySelector('[class*="Skeleton"]')).toBeInTheDocument();
    });
  });

  describe('Rate Limiting Tab', () => {
    it('displays rate limiting information when tab is clicked', async () => {
      render(<SecurityFeaturesPage />);

      const rateLimitingTab = screen.getByRole('tab', { name: /rate limiting/i });
      rateLimitingTab.click();

      await waitFor(() => {
        expect(screen.getByText('Request Rate Limiting')).toBeInTheDocument();
        expect(screen.getByText('Hash DoS Protection')).toBeInTheDocument();
        expect(screen.getByText('Bandwidth Throttling')).toBeInTheDocument();
        expect(screen.getByText('Connection Limits')).toBeInTheDocument();
      });
    });

    it('displays configuration environment variables', async () => {
      render(<SecurityFeaturesPage />);

      const rateLimitingTab = screen.getByRole('tab', { name: /rate limiting/i });
      rateLimitingTab.click();

      await waitFor(() => {
        expect(screen.getByText('NEBULAIO_FIREWALL_RATE_LIMIT_RPS')).toBeInTheDocument();
        expect(screen.getByText('NEBULAIO_FIREWALL_OBJECT_CREATION_LIMIT')).toBeInTheDocument();
      });
    });

    it('displays documentation link', async () => {
      render(<SecurityFeaturesPage />);

      const rateLimitingTab = screen.getByRole('tab', { name: /rate limiting/i });
      rateLimitingTab.click();

      await waitFor(() => {
        expect(screen.getByText('View Documentation')).toBeInTheDocument();
      });
    });
  });

  describe('Key Rotation Tab', () => {
    it('displays encryption keys table when tab is clicked', async () => {
      render(<SecurityFeaturesPage />);

      const keysTab = screen.getByRole('tab', { name: /key rotation/i });
      keysTab.click();

      await waitFor(() => {
        expect(screen.getByText('Encryption Keys')).toBeInTheDocument();
        expect(screen.getByText('master-key')).toBeInTheDocument();
        expect(screen.getByText('data-key')).toBeInTheDocument();
      });
    });

    it('displays key type badges', async () => {
      render(<SecurityFeaturesPage />);

      const keysTab = screen.getByRole('tab', { name: /key rotation/i });
      keysTab.click();

      await waitFor(() => {
        expect(screen.getByText('MASTER')).toBeInTheDocument();
        expect(screen.getByText('DATA')).toBeInTheDocument();
      });
    });

    it('shows empty state when no keys', async () => {
      server.use(
        http.get('/api/v1/keys', () => {
          return HttpResponse.json({ keys: [] });
        })
      );

      render(<SecurityFeaturesPage />);

      const keysTab = screen.getByRole('tab', { name: /key rotation/i });
      keysTab.click();

      await waitFor(() => {
        expect(screen.getByText('No encryption keys found')).toBeInTheDocument();
      });
    });
  });

  describe('mTLS Certificates Tab', () => {
    it('displays certificates table when tab is clicked', async () => {
      render(<SecurityFeaturesPage />);

      const mtlsTab = screen.getByRole('tab', { name: /mtls certificates/i });
      mtlsTab.click();

      await waitFor(() => {
        expect(screen.getByText('Certificates')).toBeInTheDocument();
        expect(screen.getByText('nebulaio-server')).toBeInTheDocument();
        expect(screen.getByText('nebulaio-ca')).toBeInTheDocument();
      });
    });

    it('displays certificate type badges', async () => {
      render(<SecurityFeaturesPage />);

      const mtlsTab = screen.getByRole('tab', { name: /mtls certificates/i });
      mtlsTab.click();

      await waitFor(() => {
        expect(screen.getByText('SERVER')).toBeInTheDocument();
        expect(screen.getByText('CA')).toBeInTheDocument();
      });
    });

    it('shows empty state when no certificates', async () => {
      server.use(
        http.get('/api/v1/mtls/certificates', () => {
          return HttpResponse.json({ certificates: [] });
        })
      );

      render(<SecurityFeaturesPage />);

      const mtlsTab = screen.getByRole('tab', { name: /mtls certificates/i });
      mtlsTab.click();

      await waitFor(() => {
        expect(screen.getByText('No certificates found')).toBeInTheDocument();
      });
    });
  });

  describe('Distributed Tracing Tab', () => {
    it('displays tracing stats when tab is clicked', async () => {
      render(<SecurityFeaturesPage />);

      const tracingTab = screen.getByRole('tab', { name: /distributed tracing/i });
      tracingTab.click();

      await waitFor(() => {
        // Check for stats that only appear in the tracing tab content
        expect(screen.getByText('Active Spans')).toBeInTheDocument();
        expect(screen.getByText('Total Spans')).toBeInTheDocument();
        expect(screen.getByText('Exporter')).toBeInTheDocument();
      });
    });

    it('displays trace propagation formats', async () => {
      render(<SecurityFeaturesPage />);

      const tracingTab = screen.getByRole('tab', { name: /distributed tracing/i });
      tracingTab.click();

      await waitFor(() => {
        expect(screen.getByText('Trace Propagation')).toBeInTheDocument();
        expect(screen.getByText('W3C Trace Context')).toBeInTheDocument();
        expect(screen.getByText('B3 (Zipkin)')).toBeInTheDocument();
        expect(screen.getByText('Jaeger')).toBeInTheDocument();
      });
    });

    it('shows alert when tracing is unavailable', async () => {
      server.use(
        http.get('/api/v1/tracing/stats', () => {
          return HttpResponse.json({ error: 'Not enabled' }, { status: 404 });
        })
      );

      render(<SecurityFeaturesPage />);

      const tracingTab = screen.getByRole('tab', { name: /distributed tracing/i });
      tracingTab.click();

      await waitFor(() => {
        expect(screen.getByText(/tracing is not enabled/i)).toBeInTheDocument();
      });
    });
  });

  describe('Configuration Tab', () => {
    it('displays configuration cards when tab is clicked', async () => {
      render(<SecurityFeaturesPage />);

      const configTab = screen.getByRole('tab', { name: /configuration/i });
      configTab.click();

      await waitFor(() => {
        // Look for configuration-specific text that only appears in config tab
        expect(screen.getByText('Anomaly Threshold (std dev)')).toBeInTheDocument();
        expect(screen.getByText('Check Interval')).toBeInTheDocument();
        expect(screen.getByText('Require Client Certificate')).toBeInTheDocument();
        expect(screen.getByText('OTLP Endpoint')).toBeInTheDocument();
      });
    });

    it('displays configuration alert', async () => {
      render(<SecurityFeaturesPage />);

      const configTab = screen.getByRole('tab', { name: /configuration/i });
      configTab.click();

      await waitFor(() => {
        expect(screen.getByText(/changes to security configuration may require a server restart/i)).toBeInTheDocument();
      });
    });
  });

  describe('Error handling', () => {
    it('handles analytics API errors gracefully', async () => {
      server.use(
        http.get('/api/v1/analytics/stats', () => {
          return HttpResponse.json({ error: 'Server error' }, { status: 500 });
        }),
        http.get('/api/v1/analytics/anomalies', () => {
          return HttpResponse.json({ error: 'Server error' }, { status: 500 });
        })
      );

      render(<SecurityFeaturesPage />);

      // Should still render the page structure
      expect(screen.getByRole('heading', { name: /security features/i })).toBeInTheDocument();
    });

    it('handles keys API errors gracefully', async () => {
      server.use(
        http.get('/api/v1/keys', () => {
          return HttpResponse.json({ error: 'Server error' }, { status: 500 });
        })
      );

      render(<SecurityFeaturesPage />);

      const keysTab = screen.getByRole('tab', { name: /key rotation/i });
      keysTab.click();

      // Should show empty state on error
      await waitFor(() => {
        expect(screen.getByText('No encryption keys found')).toBeInTheDocument();
      });
    });
  });
});
