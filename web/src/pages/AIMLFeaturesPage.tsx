import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  Title,
  Card,
  Grid,
  Text,
  Group,
  Badge,
  Switch,
  Stack,
  Paper,
  Skeleton,
  TextInput,
  NumberInput,
  Button,
  Divider,
  Code,
  ThemeIcon,
  Alert,
  Tooltip,
  ActionIcon,
  CopyButton,
  Tabs,
  SimpleGrid,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import {
  IconBolt,
  IconDatabase,
  IconRobot,
  IconCpu,
  IconNetwork,
  IconBrain,
  IconServer,
  IconCheck,
  IconInfoCircle,
  IconCopy,
  IconExternalLink,
  IconSettings,
  IconChartBar,
  IconRefresh,
} from '@tabler/icons-react';
import { adminApi } from '../api/client';

interface FeatureConfig {
  enabled: boolean;
  [key: string]: unknown;
}

interface AIMLFeatures {
  s3_express: FeatureConfig & {
    default_zone: string;
    enable_atomic_append: boolean;
  };
  iceberg: FeatureConfig & {
    catalog_type: string;
    warehouse: string;
    enable_acid: boolean;
  };
  mcp: FeatureConfig & {
    port: number;
    enable_tools: boolean;
    enable_resources: boolean;
  };
  gpudirect: FeatureConfig & {
    buffer_pool_size: number;
    enable_async: boolean;
  };
  dpu: FeatureConfig & {
    device_name: string;
    enable_crypto: boolean;
    enable_compression: boolean;
  };
  rdma: FeatureConfig & {
    port: number;
    device_name: string;
    enable_zero_copy: boolean;
  };
  nim: FeatureConfig & {
    endpoint: string;
    default_model: string;
    enable_streaming: boolean;
  };
}

interface AIMLMetrics {
  s3_express: {
    enabled: boolean;
    sessions_created: number;
    sessions_active: number;
    put_operations: number;
    put_bytes_written: number;
    list_operations: number;
    append_operations: number;
    append_conflicts: number;
    lightweight_etags: number;
    avg_put_latency_ms: number;
  };
  iceberg: {
    enabled: boolean;
    namespaces_total: number;
    tables_total: number;
    commits_succeeded: number;
    commits_failed: number;
    snapshots_created: number;
    cache_hits: number;
    cache_misses: number;
    cache_hit_rate: number;
  };
  mcp: {
    enabled: boolean;
    requests_total: number;
    requests_success: number;
    requests_failed: number;
    tool_invocations: number;
    active_sessions: number;
    bytes_transferred: number;
    avg_latency_ms: number;
  };
  gpudirect: {
    enabled: boolean;
    read_ops: number;
    write_ops: number;
    read_bytes: number;
    write_bytes: number;
    buffer_hits: number;
    buffer_misses: number;
    buffer_hit_rate: number;
    fallback_ops: number;
    errors: number;
  };
  dpu: {
    enabled: boolean;
    crypto_ops: number;
    compress_ops: number;
    crypto_bytes: number;
    compress_bytes: number;
    fallbacks: number;
    errors: number;
  };
  rdma: {
    enabled: boolean;
    connections_active: number;
    requests_total: number;
    requests_success: number;
    bytes_sent: number;
    bytes_received: number;
    avg_latency_us: number;
  };
  nim: {
    enabled: boolean;
    requests_total: number;
    requests_success: number;
    tokens_used: number;
    cache_hits: number;
    cache_misses: number;
    cache_hit_rate: number;
    avg_latency_ms: number;
  };
}

const defaultFeatures: AIMLFeatures = {
  s3_express: {
    enabled: false,
    default_zone: 'use1-az1',
    enable_atomic_append: true,
  },
  iceberg: {
    enabled: false,
    catalog_type: 'rest',
    warehouse: 's3://warehouse/',
    enable_acid: true,
  },
  mcp: {
    enabled: false,
    port: 9005,
    enable_tools: true,
    enable_resources: true,
  },
  gpudirect: {
    enabled: false,
    buffer_pool_size: 1073741824,
    enable_async: true,
  },
  dpu: {
    enabled: false,
    device_name: 'mlx5_0',
    enable_crypto: true,
    enable_compression: true,
  },
  rdma: {
    enabled: false,
    port: 9100,
    device_name: 'mlx5_0',
    enable_zero_copy: true,
  },
  nim: {
    enabled: false,
    endpoint: 'https://integrate.api.nvidia.com/v1',
    default_model: 'meta/llama-3.1-8b-instruct',
    enable_streaming: true,
  },
};

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

function formatNumber(num: number): string {
  if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M';
  if (num >= 1000) return (num / 1000).toFixed(1) + 'K';
  return num.toString();
}

function MetricCard({
  title,
  value,
  subtitle,
  color = 'blue',
}: {
  title: string;
  value: string | number;
  subtitle?: string;
  color?: string;
}) {
  return (
    <Card withBorder p="md" radius="md">
      <Text size="xs" c="dimmed" tt="uppercase" fw={700}>
        {title}
      </Text>
      <Text fw={700} size="xl" c={color}>
        {value}
      </Text>
      {subtitle && (
        <Text size="xs" c="dimmed">
          {subtitle}
        </Text>
      )}
    </Card>
  );
}

function FeatureCard({
  title,
  description,
  icon,
  enabled,
  onToggle,
  children,
  docsLink,
}: {
  title: string;
  description: string;
  icon: React.ReactNode;
  enabled: boolean;
  onToggle: (enabled: boolean) => void;
  children?: React.ReactNode;
  docsLink?: string;
}) {
  return (
    <Card withBorder radius="md" p="lg">
      <Group justify="space-between" mb="md">
        <Group>
          <ThemeIcon size="lg" variant="light" color={enabled ? 'blue' : 'gray'}>
            {icon}
          </ThemeIcon>
          <div>
            <Group gap="xs">
              <Text fw={500}>{title}</Text>
              {docsLink && (
                <Tooltip label="View documentation">
                  <ActionIcon
                    variant="subtle"
                    size="sm"
                    component="a"
                    href={docsLink}
                    target="_blank"
                  >
                    <IconExternalLink size={14} />
                  </ActionIcon>
                </Tooltip>
              )}
            </Group>
            <Text size="sm" c="dimmed">
              {description}
            </Text>
          </div>
        </Group>
        <Switch
          checked={enabled}
          onChange={(e) => onToggle(e.currentTarget.checked)}
          size="md"
          onLabel="ON"
          offLabel="OFF"
        />
      </Group>
      {enabled && children && (
        <>
          <Divider my="sm" />
          {children}
        </>
      )}
    </Card>
  );
}

function MetricsDashboard({ metrics }: { metrics: AIMLMetrics | null }) {
  if (!metrics) {
    return (
      <Stack>
        <Skeleton height={100} />
        <Skeleton height={100} />
        <Skeleton height={100} />
      </Stack>
    );
  }

  return (
    <Stack>
      {/* Overview Cards */}
      <SimpleGrid cols={{ base: 2, sm: 4, md: 7 }}>
        <MetricCard
          title="S3 Express"
          value={metrics.s3_express.enabled ? 'ON' : 'OFF'}
          subtitle={metrics.s3_express.enabled ? `${metrics.s3_express.sessions_active} sessions` : undefined}
          color={metrics.s3_express.enabled ? 'green' : 'gray'}
        />
        <MetricCard
          title="Iceberg"
          value={metrics.iceberg.enabled ? 'ON' : 'OFF'}
          subtitle={metrics.iceberg.enabled ? `${metrics.iceberg.tables_total} tables` : undefined}
          color={metrics.iceberg.enabled ? 'green' : 'gray'}
        />
        <MetricCard
          title="MCP"
          value={metrics.mcp.enabled ? 'ON' : 'OFF'}
          subtitle={metrics.mcp.enabled ? `${metrics.mcp.active_sessions} sessions` : undefined}
          color={metrics.mcp.enabled ? 'green' : 'gray'}
        />
        <MetricCard
          title="GPUDirect"
          value={metrics.gpudirect.enabled ? 'ON' : 'OFF'}
          subtitle={metrics.gpudirect.enabled ? `${metrics.gpudirect.read_ops + metrics.gpudirect.write_ops} ops` : undefined}
          color={metrics.gpudirect.enabled ? 'green' : 'gray'}
        />
        <MetricCard
          title="DPU"
          value={metrics.dpu.enabled ? 'ON' : 'OFF'}
          subtitle={metrics.dpu.enabled ? `${metrics.dpu.crypto_ops + metrics.dpu.compress_ops} ops` : undefined}
          color={metrics.dpu.enabled ? 'green' : 'gray'}
        />
        <MetricCard
          title="RDMA"
          value={metrics.rdma.enabled ? 'ON' : 'OFF'}
          subtitle={metrics.rdma.enabled ? `${metrics.rdma.connections_active} conns` : undefined}
          color={metrics.rdma.enabled ? 'green' : 'gray'}
        />
        <MetricCard
          title="NIM"
          value={metrics.nim.enabled ? 'ON' : 'OFF'}
          subtitle={metrics.nim.enabled ? `${formatNumber(metrics.nim.tokens_used)} tokens` : undefined}
          color={metrics.nim.enabled ? 'green' : 'gray'}
        />
      </SimpleGrid>

      {/* Detailed Feature Metrics */}
      <Grid>
        {/* S3 Express Metrics */}
        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder p="md">
            <Group justify="space-between" mb="md">
              <Group>
                <ThemeIcon color="yellow" variant="light">
                  <IconBolt size={16} />
                </ThemeIcon>
                <Text fw={500}>S3 Express One Zone</Text>
              </Group>
              <Badge color={metrics.s3_express.enabled ? 'green' : 'gray'}>
                {metrics.s3_express.enabled ? 'Enabled' : 'Disabled'}
              </Badge>
            </Group>
            <SimpleGrid cols={3}>
              <div>
                <Text size="xs" c="dimmed">PUT Operations</Text>
                <Text fw={500}>{formatNumber(metrics.s3_express.put_operations)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Append Ops</Text>
                <Text fw={500}>{formatNumber(metrics.s3_express.append_operations)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Conflicts</Text>
                <Text fw={500}>{metrics.s3_express.append_conflicts}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Bytes Written</Text>
                <Text fw={500}>{formatBytes(metrics.s3_express.put_bytes_written)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Avg Latency</Text>
                <Text fw={500}>{metrics.s3_express.avg_put_latency_ms.toFixed(2)}ms</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Sessions</Text>
                <Text fw={500}>{metrics.s3_express.sessions_active}</Text>
              </div>
            </SimpleGrid>
          </Card>
        </Grid.Col>

        {/* Iceberg Metrics */}
        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder p="md">
            <Group justify="space-between" mb="md">
              <Group>
                <ThemeIcon color="cyan" variant="light">
                  <IconDatabase size={16} />
                </ThemeIcon>
                <Text fw={500}>Apache Iceberg</Text>
              </Group>
              <Badge color={metrics.iceberg.enabled ? 'green' : 'gray'}>
                {metrics.iceberg.enabled ? 'Enabled' : 'Disabled'}
              </Badge>
            </Group>
            <SimpleGrid cols={3}>
              <div>
                <Text size="xs" c="dimmed">Tables</Text>
                <Text fw={500}>{metrics.iceberg.tables_total}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Namespaces</Text>
                <Text fw={500}>{metrics.iceberg.namespaces_total}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Snapshots</Text>
                <Text fw={500}>{formatNumber(metrics.iceberg.snapshots_created)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Commits OK</Text>
                <Text fw={500} c="green">{formatNumber(metrics.iceberg.commits_succeeded)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Commits Failed</Text>
                <Text fw={500} c="red">{metrics.iceberg.commits_failed}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Cache Hit Rate</Text>
                <Text fw={500}>{(metrics.iceberg.cache_hit_rate * 100).toFixed(1)}%</Text>
              </div>
            </SimpleGrid>
          </Card>
        </Grid.Col>

        {/* MCP Metrics */}
        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder p="md">
            <Group justify="space-between" mb="md">
              <Group>
                <ThemeIcon color="violet" variant="light">
                  <IconRobot size={16} />
                </ThemeIcon>
                <Text fw={500}>MCP Server</Text>
              </Group>
              <Badge color={metrics.mcp.enabled ? 'green' : 'gray'}>
                {metrics.mcp.enabled ? 'Enabled' : 'Disabled'}
              </Badge>
            </Group>
            <SimpleGrid cols={3}>
              <div>
                <Text size="xs" c="dimmed">Total Requests</Text>
                <Text fw={500}>{formatNumber(metrics.mcp.requests_total)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Success Rate</Text>
                <Text fw={500} c="green">
                  {metrics.mcp.requests_total > 0
                    ? ((metrics.mcp.requests_success / metrics.mcp.requests_total) * 100).toFixed(1)
                    : 0}%
                </Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Tool Calls</Text>
                <Text fw={500}>{formatNumber(metrics.mcp.tool_invocations)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Active Sessions</Text>
                <Text fw={500}>{metrics.mcp.active_sessions}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Data Transferred</Text>
                <Text fw={500}>{formatBytes(metrics.mcp.bytes_transferred)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Avg Latency</Text>
                <Text fw={500}>{metrics.mcp.avg_latency_ms.toFixed(2)}ms</Text>
              </div>
            </SimpleGrid>
          </Card>
        </Grid.Col>

        {/* GPUDirect Metrics */}
        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder p="md">
            <Group justify="space-between" mb="md">
              <Group>
                <ThemeIcon color="green" variant="light">
                  <IconCpu size={16} />
                </ThemeIcon>
                <Text fw={500}>GPUDirect Storage</Text>
              </Group>
              <Badge color={metrics.gpudirect.enabled ? 'green' : 'gray'}>
                {metrics.gpudirect.enabled ? 'Enabled' : 'Disabled'}
              </Badge>
            </Group>
            <SimpleGrid cols={3}>
              <div>
                <Text size="xs" c="dimmed">Read Ops</Text>
                <Text fw={500}>{formatNumber(metrics.gpudirect.read_ops)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Write Ops</Text>
                <Text fw={500}>{formatNumber(metrics.gpudirect.write_ops)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Fallbacks</Text>
                <Text fw={500} c={metrics.gpudirect.fallback_ops > 0 ? 'orange' : undefined}>
                  {metrics.gpudirect.fallback_ops}
                </Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Bytes Read</Text>
                <Text fw={500}>{formatBytes(metrics.gpudirect.read_bytes)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Bytes Written</Text>
                <Text fw={500}>{formatBytes(metrics.gpudirect.write_bytes)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Buffer Hit Rate</Text>
                <Text fw={500}>{(metrics.gpudirect.buffer_hit_rate * 100).toFixed(1)}%</Text>
              </div>
            </SimpleGrid>
            {metrics.gpudirect.errors > 0 && (
              <Alert color="red" mt="sm" icon={<IconInfoCircle size={14} />}>
                {metrics.gpudirect.errors} errors detected
              </Alert>
            )}
          </Card>
        </Grid.Col>

        {/* DPU Metrics */}
        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder p="md">
            <Group justify="space-between" mb="md">
              <Group>
                <ThemeIcon color="orange" variant="light">
                  <IconServer size={16} />
                </ThemeIcon>
                <Text fw={500}>BlueField DPU</Text>
              </Group>
              <Badge color={metrics.dpu.enabled ? 'green' : 'gray'}>
                {metrics.dpu.enabled ? 'Enabled' : 'Disabled'}
              </Badge>
            </Group>
            <SimpleGrid cols={3}>
              <div>
                <Text size="xs" c="dimmed">Crypto Ops</Text>
                <Text fw={500}>{formatNumber(metrics.dpu.crypto_ops)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Compress Ops</Text>
                <Text fw={500}>{formatNumber(metrics.dpu.compress_ops)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Fallbacks</Text>
                <Text fw={500} c={metrics.dpu.fallbacks > 0 ? 'orange' : undefined}>
                  {metrics.dpu.fallbacks}
                </Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Crypto Bytes</Text>
                <Text fw={500}>{formatBytes(metrics.dpu.crypto_bytes)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Compress Bytes</Text>
                <Text fw={500}>{formatBytes(metrics.dpu.compress_bytes)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Errors</Text>
                <Text fw={500} c={metrics.dpu.errors > 0 ? 'red' : undefined}>
                  {metrics.dpu.errors}
                </Text>
              </div>
            </SimpleGrid>
          </Card>
        </Grid.Col>

        {/* RDMA Metrics */}
        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder p="md">
            <Group justify="space-between" mb="md">
              <Group>
                <ThemeIcon color="indigo" variant="light">
                  <IconNetwork size={16} />
                </ThemeIcon>
                <Text fw={500}>S3 over RDMA</Text>
              </Group>
              <Badge color={metrics.rdma.enabled ? 'green' : 'gray'}>
                {metrics.rdma.enabled ? 'Enabled' : 'Disabled'}
              </Badge>
            </Group>
            <SimpleGrid cols={3}>
              <div>
                <Text size="xs" c="dimmed">Active Conns</Text>
                <Text fw={500}>{metrics.rdma.connections_active}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Total Requests</Text>
                <Text fw={500}>{formatNumber(metrics.rdma.requests_total)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Success Rate</Text>
                <Text fw={500} c="green">
                  {metrics.rdma.requests_total > 0
                    ? ((metrics.rdma.requests_success / metrics.rdma.requests_total) * 100).toFixed(1)
                    : 0}%
                </Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Bytes Sent</Text>
                <Text fw={500}>{formatBytes(metrics.rdma.bytes_sent)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Bytes Received</Text>
                <Text fw={500}>{formatBytes(metrics.rdma.bytes_received)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Avg Latency</Text>
                <Text fw={500}>{metrics.rdma.avg_latency_us.toFixed(2)}μs</Text>
              </div>
            </SimpleGrid>
          </Card>
        </Grid.Col>

        {/* NIM Metrics */}
        <Grid.Col span={12}>
          <Card withBorder p="md">
            <Group justify="space-between" mb="md">
              <Group>
                <ThemeIcon color="grape" variant="light">
                  <IconBrain size={16} />
                </ThemeIcon>
                <Text fw={500}>NVIDIA NIM Integration</Text>
              </Group>
              <Badge color={metrics.nim.enabled ? 'green' : 'gray'}>
                {metrics.nim.enabled ? 'Enabled' : 'Disabled'}
              </Badge>
            </Group>
            <SimpleGrid cols={{ base: 3, md: 6 }}>
              <div>
                <Text size="xs" c="dimmed">Total Requests</Text>
                <Text fw={500}>{formatNumber(metrics.nim.requests_total)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Success Rate</Text>
                <Text fw={500} c="green">
                  {metrics.nim.requests_total > 0
                    ? ((metrics.nim.requests_success / metrics.nim.requests_total) * 100).toFixed(1)
                    : 0}%
                </Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Tokens Used</Text>
                <Text fw={500}>{formatNumber(metrics.nim.tokens_used)}</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Cache Hit Rate</Text>
                <Text fw={500}>{(metrics.nim.cache_hit_rate * 100).toFixed(1)}%</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Avg Latency</Text>
                <Text fw={500}>{metrics.nim.avg_latency_ms.toFixed(0)}ms</Text>
              </div>
              <div>
                <Text size="xs" c="dimmed">Cache Hits/Misses</Text>
                <Text fw={500}>{metrics.nim.cache_hits}/{metrics.nim.cache_misses}</Text>
              </div>
            </SimpleGrid>
          </Card>
        </Grid.Col>
      </Grid>
    </Stack>
  );
}

function ConfigurationPanel({
  features,
  displayFeatures,
  toggleFeature,
  updateFeature,
}: {
  features: AIMLFeatures;
  displayFeatures: AIMLFeatures;
  toggleFeature: (feature: keyof AIMLFeatures, enabled: boolean) => void;
  updateFeature: <K extends keyof AIMLFeatures>(
    feature: K,
    key: keyof AIMLFeatures[K],
    value: unknown
  ) => void;
}) {
  return (
    <Grid>
      {/* S3 Express One Zone */}
      <Grid.Col span={{ base: 12, md: 6 }}>
        <FeatureCard
          title="S3 Express One Zone"
          description="Ultra-low latency storage with atomic appends"
          icon={<IconBolt size={20} />}
          enabled={displayFeatures.s3_express.enabled}
          onToggle={(v) => toggleFeature('s3_express', v)}
          docsLink="/docs/enterprise/s3-express"
        >
          <Stack gap="sm">
            <TextInput
              label="Default Zone"
              value={features.s3_express.default_zone}
              onChange={(e) =>
                updateFeature('s3_express', 'default_zone', e.target.value)
              }
              description="Default availability zone for Express buckets"
            />
            <Switch
              label="Enable Atomic Append"
              checked={features.s3_express.enable_atomic_append}
              onChange={(e) =>
                updateFeature('s3_express', 'enable_atomic_append', e.currentTarget.checked)
              }
            />
          </Stack>
        </FeatureCard>
      </Grid.Col>

      {/* Apache Iceberg */}
      <Grid.Col span={{ base: 12, md: 6 }}>
        <FeatureCard
          title="Apache Iceberg"
          description="Native table format for data lakehouse workloads"
          icon={<IconDatabase size={20} />}
          enabled={displayFeatures.iceberg.enabled}
          onToggle={(v) => toggleFeature('iceberg', v)}
          docsLink="/docs/enterprise/iceberg"
        >
          <Stack gap="sm">
            <TextInput
              label="Warehouse Location"
              value={features.iceberg.warehouse}
              onChange={(e) => updateFeature('iceberg', 'warehouse', e.target.value)}
              description="S3 path for Iceberg warehouse"
            />
            <Switch
              label="Enable ACID Transactions"
              checked={features.iceberg.enable_acid}
              onChange={(e) =>
                updateFeature('iceberg', 'enable_acid', e.currentTarget.checked)
              }
            />
            <Group>
              <Text size="sm" c="dimmed">
                REST Catalog:
              </Text>
              <Code>http://localhost:9006/iceberg</Code>
            </Group>
          </Stack>
        </FeatureCard>
      </Grid.Col>

      {/* MCP Server */}
      <Grid.Col span={{ base: 12, md: 6 }}>
        <FeatureCard
          title="MCP Server"
          description="AI agent integration (Claude, ChatGPT, etc.)"
          icon={<IconRobot size={20} />}
          enabled={displayFeatures.mcp.enabled}
          onToggle={(v) => toggleFeature('mcp', v)}
          docsLink="/docs/enterprise/mcp-server"
        >
          <Stack gap="sm">
            <NumberInput
              label="Port"
              value={features.mcp.port}
              onChange={(v) => updateFeature('mcp', 'port', v)}
              min={1024}
              max={65535}
            />
            <Switch
              label="Enable Tools"
              checked={features.mcp.enable_tools}
              onChange={(e) =>
                updateFeature('mcp', 'enable_tools', e.currentTarget.checked)
              }
            />
            <Switch
              label="Enable Resources"
              checked={features.mcp.enable_resources}
              onChange={(e) =>
                updateFeature('mcp', 'enable_resources', e.currentTarget.checked)
              }
            />
            <Group>
              <Text size="sm" c="dimmed">
                Endpoint:
              </Text>
              <Code>http://localhost:{features.mcp.port}/mcp</Code>
              <CopyButton value={`http://localhost:${features.mcp.port}/mcp`}>
                {({ copied, copy }) => (
                  <ActionIcon variant="subtle" size="sm" onClick={copy}>
                    {copied ? <IconCheck size={14} /> : <IconCopy size={14} />}
                  </ActionIcon>
                )}
              </CopyButton>
            </Group>
          </Stack>
        </FeatureCard>
      </Grid.Col>

      {/* GPUDirect Storage */}
      <Grid.Col span={{ base: 12, md: 6 }}>
        <FeatureCard
          title="GPUDirect Storage"
          description="Zero-copy GPU-to-storage transfers"
          icon={<IconCpu size={20} />}
          enabled={displayFeatures.gpudirect.enabled}
          onToggle={(v) => toggleFeature('gpudirect', v)}
          docsLink="/docs/enterprise/gpudirect"
        >
          <Stack gap="sm">
            <NumberInput
              label="Buffer Pool Size (bytes)"
              value={features.gpudirect.buffer_pool_size}
              onChange={(v) => updateFeature('gpudirect', 'buffer_pool_size', v)}
              min={1024 * 1024}
              step={1024 * 1024}
              description="Size of GPU buffer pool"
            />
            <Switch
              label="Enable Async Operations"
              checked={features.gpudirect.enable_async}
              onChange={(e) =>
                updateFeature('gpudirect', 'enable_async', e.currentTarget.checked)
              }
            />
            <Alert color="yellow" icon={<IconInfoCircle size={14} />}>
              Requires NVIDIA GPU with GDS support
            </Alert>
          </Stack>
        </FeatureCard>
      </Grid.Col>

      {/* BlueField DPU */}
      <Grid.Col span={{ base: 12, md: 6 }}>
        <FeatureCard
          title="BlueField DPU"
          description="SmartNIC offload for crypto/compression"
          icon={<IconServer size={20} />}
          enabled={displayFeatures.dpu.enabled}
          onToggle={(v) => toggleFeature('dpu', v)}
          docsLink="/docs/enterprise/dpu"
        >
          <Stack gap="sm">
            <TextInput
              label="Device Name"
              value={features.dpu.device_name}
              onChange={(e) => updateFeature('dpu', 'device_name', e.target.value)}
              description="RDMA device name (e.g., mlx5_0)"
            />
            <Switch
              label="Enable Crypto Offload"
              checked={features.dpu.enable_crypto}
              onChange={(e) =>
                updateFeature('dpu', 'enable_crypto', e.currentTarget.checked)
              }
            />
            <Switch
              label="Enable Compression Offload"
              checked={features.dpu.enable_compression}
              onChange={(e) =>
                updateFeature('dpu', 'enable_compression', e.currentTarget.checked)
              }
            />
            <Alert color="yellow" icon={<IconInfoCircle size={14} />}>
              Requires NVIDIA BlueField DPU
            </Alert>
          </Stack>
        </FeatureCard>
      </Grid.Col>

      {/* S3 over RDMA */}
      <Grid.Col span={{ base: 12, md: 6 }}>
        <FeatureCard
          title="S3 over RDMA"
          description="Sub-10μs latency via InfiniBand/RoCE"
          icon={<IconNetwork size={20} />}
          enabled={displayFeatures.rdma.enabled}
          onToggle={(v) => toggleFeature('rdma', v)}
          docsLink="/docs/enterprise/rdma"
        >
          <Stack gap="sm">
            <NumberInput
              label="Port"
              value={features.rdma.port}
              onChange={(v) => updateFeature('rdma', 'port', v)}
              min={1024}
              max={65535}
            />
            <TextInput
              label="Device Name"
              value={features.rdma.device_name}
              onChange={(e) => updateFeature('rdma', 'device_name', e.target.value)}
              description="InfiniBand/RoCE device name"
            />
            <Switch
              label="Enable Zero-Copy"
              checked={features.rdma.enable_zero_copy}
              onChange={(e) =>
                updateFeature('rdma', 'enable_zero_copy', e.currentTarget.checked)
              }
            />
            <Alert color="yellow" icon={<IconInfoCircle size={14} />}>
              Requires InfiniBand or RoCE v2 network
            </Alert>
          </Stack>
        </FeatureCard>
      </Grid.Col>

      {/* NVIDIA NIM */}
      <Grid.Col span={12}>
        <FeatureCard
          title="NVIDIA NIM Integration"
          description="AI inference on stored objects"
          icon={<IconBrain size={20} />}
          enabled={displayFeatures.nim.enabled}
          onToggle={(v) => toggleFeature('nim', v)}
          docsLink="/docs/enterprise/nim"
        >
          <Grid>
            <Grid.Col span={{ base: 12, md: 6 }}>
              <Stack gap="sm">
                <TextInput
                  label="NIM Endpoint"
                  value={features.nim.endpoint}
                  onChange={(e) => updateFeature('nim', 'endpoint', e.target.value)}
                  description="NVIDIA NIM API endpoint"
                />
                <TextInput
                  label="Default Model"
                  value={features.nim.default_model}
                  onChange={(e) => updateFeature('nim', 'default_model', e.target.value)}
                  description="Default model for inference"
                />
              </Stack>
            </Grid.Col>
            <Grid.Col span={{ base: 12, md: 6 }}>
              <Stack gap="sm">
                <Switch
                  label="Enable Streaming"
                  checked={features.nim.enable_streaming}
                  onChange={(e) =>
                    updateFeature('nim', 'enable_streaming', e.currentTarget.checked)
                  }
                />
                <Alert color="blue" icon={<IconInfoCircle size={14} />}>
                  <Text size="sm">
                    Get your API key from{' '}
                    <Text
                      component="a"
                      href="https://build.nvidia.com"
                      target="_blank"
                      c="blue"
                    >
                      build.nvidia.com
                    </Text>
                  </Text>
                </Alert>
                <Group>
                  <Badge color="green" variant="light">
                    LLM
                  </Badge>
                  <Badge color="violet" variant="light">
                    Vision
                  </Badge>
                  <Badge color="orange" variant="light">
                    Audio
                  </Badge>
                  <Badge color="blue" variant="light">
                    Embedding
                  </Badge>
                </Group>
              </Stack>
            </Grid.Col>
          </Grid>
        </FeatureCard>
      </Grid.Col>
    </Grid>
  );
}

export function AIMLFeaturesPage() {
  const queryClient = useQueryClient();
  const [features, setFeatures] = useState<AIMLFeatures>(defaultFeatures);
  const [activeTab, setActiveTab] = useState<string | null>('metrics');

  const { data: serverFeatures, isLoading: featuresLoading } = useQuery({
    queryKey: ['aiml-features'],
    queryFn: async () => {
      try {
        const res = await adminApi.getConfig();
        return res.data as AIMLFeatures;
      } catch {
        return defaultFeatures;
      }
    },
  });

  const { data: metrics, isLoading: metricsLoading, refetch: refetchMetrics } = useQuery({
    queryKey: ['aiml-metrics'],
    queryFn: async () => {
      try {
        const res = await adminApi.getAIMLMetrics();
        return res.data as AIMLMetrics;
      } catch {
        return null;
      }
    },
    refetchInterval: 30000, // Refresh every 30 seconds
  });

  const updateMutation = useMutation({
    mutationFn: (newFeatures: AIMLFeatures) => adminApi.updateConfig(newFeatures),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['aiml-features'] });
      notifications.show({
        title: 'Settings updated',
        message: 'AI/ML feature settings have been saved. Restart may be required.',
        color: 'green',
      });
    },
    onError: () => {
      notifications.show({
        title: 'Error',
        message: 'Failed to update settings',
        color: 'red',
      });
    },
  });

  const updateFeature = <K extends keyof AIMLFeatures>(
    feature: K,
    key: keyof AIMLFeatures[K],
    value: unknown
  ) => {
    setFeatures((prev) => ({
      ...prev,
      [feature]: {
        ...prev[feature],
        [key]: value,
      },
    }));
  };

  const toggleFeature = (feature: keyof AIMLFeatures, enabled: boolean) => {
    updateFeature(feature, 'enabled', enabled);
  };

  const displayFeatures = serverFeatures || features;

  return (
    <div>
      <Group justify="space-between" mb="lg">
        <div>
          <Title order={2}>AI/ML Features</Title>
          <Text c="dimmed" size="sm">
            Configure and monitor advanced AI and machine learning capabilities
          </Text>
        </div>
        <Group>
          {activeTab === 'metrics' && (
            <Button
              variant="default"
              leftSection={<IconRefresh size={16} />}
              onClick={() => refetchMetrics()}
              loading={metricsLoading}
            >
              Refresh
            </Button>
          )}
          {activeTab === 'config' && (
            <>
              <Button
                variant="default"
                onClick={() => setFeatures(serverFeatures || defaultFeatures)}
              >
                Reset
              </Button>
              <Button
                onClick={() => updateMutation.mutate(features)}
                loading={updateMutation.isPending}
              >
                Save Changes
              </Button>
            </>
          )}
        </Group>
      </Group>

      <Tabs value={activeTab} onChange={setActiveTab} mb="lg">
        <Tabs.List>
          <Tabs.Tab value="metrics" leftSection={<IconChartBar size={16} />}>
            Metrics Dashboard
          </Tabs.Tab>
          <Tabs.Tab value="config" leftSection={<IconSettings size={16} />}>
            Configuration
          </Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value="metrics" pt="md">
          <MetricsDashboard metrics={metrics ?? null} />
        </Tabs.Panel>

        <Tabs.Panel value="config" pt="md">
          <Alert icon={<IconInfoCircle size={16} />} color="blue" mb="lg">
            Some features require specific hardware (GPU, RDMA NIC, DPU) or external services
            (NVIDIA NIM). Changes may require a server restart to take effect.
          </Alert>

          {featuresLoading ? (
            <Stack>
              <Skeleton height={200} />
              <Skeleton height={200} />
              <Skeleton height={200} />
            </Stack>
          ) : (
            <ConfigurationPanel
              features={features}
              displayFeatures={displayFeatures}
              toggleFeature={toggleFeature}
              updateFeature={updateFeature}
            />
          )}
        </Tabs.Panel>
      </Tabs>

      {/* Quick Reference */}
      <Paper withBorder p="lg" mt="lg">
        <Text fw={500} mb="md">
          Quick Reference - Ports & Endpoints
        </Text>
        <Grid>
          <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
            <Group>
              <Badge color="blue" variant="light">
                S3 API
              </Badge>
              <Code>:9000</Code>
            </Group>
          </Grid.Col>
          <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
            <Group>
              <Badge color="green" variant="light">
                MCP Server
              </Badge>
              <Code>:9005</Code>
            </Group>
          </Grid.Col>
          <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
            <Group>
              <Badge color="violet" variant="light">
                Iceberg
              </Badge>
              <Code>:9006</Code>
            </Group>
          </Grid.Col>
          <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
            <Group>
              <Badge color="orange" variant="light">
                RDMA
              </Badge>
              <Code>:9100</Code>
            </Group>
          </Grid.Col>
        </Grid>
      </Paper>
    </div>
  );
}
