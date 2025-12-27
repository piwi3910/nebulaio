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

export function AIMLFeaturesPage() {
  const queryClient = useQueryClient();
  const [features, setFeatures] = useState<AIMLFeatures>(defaultFeatures);

  const { data: serverFeatures, isLoading } = useQuery({
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

  // Use server features if loaded
  const displayFeatures = serverFeatures || features;

  return (
    <div>
      <Group justify="space-between" mb="lg">
        <div>
          <Title order={2}>AI/ML Features</Title>
          <Text c="dimmed" size="sm">
            Configure advanced AI and machine learning capabilities
          </Text>
        </div>
        <Group>
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
        </Group>
      </Group>

      <Alert icon={<IconInfoCircle size={16} />} color="blue" mb="lg">
        Some features require specific hardware (GPU, RDMA NIC, DPU) or external services
        (NVIDIA NIM). Changes may require a server restart to take effect.
      </Alert>

      {isLoading ? (
        <Stack>
          <Skeleton height={200} />
          <Skeleton height={200} />
          <Skeleton height={200} />
        </Stack>
      ) : (
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
      )}

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
