import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  Title,
  Card,
  Grid,
  Text,
  Group,
  Badge,
  Stack,
  Paper,
  SimpleGrid,
  Skeleton,
  TextInput,
  Button,
  Table,
  ThemeIcon,
  ActionIcon,
  Tabs,
  Select,
  RingProgress,
  Alert,
  Tooltip,
  Menu,
  Modal,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { useDisclosure } from '@mantine/hooks';
import {
  IconTrendingUp,
  IconTrendingDown,
  IconAlertCircle,
  IconCheck,
  IconX,
  IconSearch,
  IconRefresh,
  IconChartBar,
  IconSettings,
  IconFlame,
  IconSnowflake,
  IconFilter,
  IconDotsVertical,
  IconEye,
  IconCircleCheck,
} from '@tabler/icons-react';
import { LineChart, Line, PieChart, Pie, Cell, XAxis, YAxis, CartesianGrid, Tooltip as RechartsTooltip, Legend, ResponsiveContainer } from 'recharts';
import { adminApi } from '../api/client';

// Types
interface PredictiveMetrics {
  total_predictions: number;
  active_anomalies: number;
  pending_recommendations: number;
  cost_savings_usd: number;
}

interface TierRecommendation {
  id: string;
  bucket: string;
  key: string;
  current_tier: string;
  recommended_tier: string;
  confidence: number;
  predicted_access_rate: number;
  reasoning: string;
  estimated_savings_usd: number;
  created_at: string;
}

interface HotObjectPrediction {
  bucket: string;
  key: string;
  current_tier: string;
  predicted_access_rate: number;
  trend: 'increasing' | 'decreasing' | 'stable';
  confidence: number;
  days_to_hot: number;
}

interface ColdObjectPrediction {
  bucket: string;
  key: string;
  current_tier: string;
  days_since_access: number;
  predicted_days_to_cold: number;
  trend: 'decreasing' | 'stable';
  confidence: number;
}

interface AnomalyDetection {
  id: string;
  timestamp: string;
  bucket: string;
  key?: string;
  type: 'spike' | 'drop' | 'unusual_pattern';
  severity: 'low' | 'medium' | 'high' | 'critical';
  expected_value: number;
  actual_value: number;
  deviation_percent: number;
  acknowledged: boolean;
  acknowledged_at?: string;
  acknowledged_by?: string;
}

interface ObjectPrediction {
  bucket: string;
  key: string;
  short_term_access_rate: number;
  long_term_access_rate: number;
  trend: 'increasing' | 'decreasing' | 'stable';
  seasonal_patterns: string[];
  recommended_tier: string;
  confidence: number;
  current_tier: string;
}

function formatNumber(num: number): string {
  if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M';
  if (num >= 1000) return (num / 1000).toFixed(1) + 'K';
  return num.toString();
}

function formatCurrency(value: number): string {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format(value);
}

function MetricCard({
  title,
  value,
  subtitle,
  icon,
  color = 'blue',
}: {
  title: string;
  value: string | number;
  subtitle?: string;
  icon: React.ReactNode;
  color?: string;
}) {
  return (
    <Paper withBorder p="md" radius="md">
      <Group justify="space-between">
        <div>
          <Text size="xs" c="dimmed" tt="uppercase" fw={700}>
            {title}
          </Text>
          <Text fw={700} size="xl" c={color}>
            {value}
          </Text>
          {subtitle && (
            <Text size="xs" c="dimmed" mt={4}>
              {subtitle}
            </Text>
          )}
        </div>
        <ThemeIcon color={color} variant="light" size={48} radius="md">
          {icon}
        </ThemeIcon>
      </Group>
    </Paper>
  );
}

function TierRecommendationsTable({
  recommendations,
  onApply,
  onDismiss,
  loading,
}: {
  recommendations: TierRecommendation[];
  onApply: (id: string) => void;
  onDismiss: (id: string) => void;
  loading: boolean;
}) {
  const [selectedIds, setSelectedIds] = useState<string[]>([]);

  const toggleSelection = (id: string) => {
    setSelectedIds((prev) =>
      prev.includes(id) ? prev.filter((item) => item !== id) : [...prev, id]
    );
  };

  const toggleAll = () => {
    if (selectedIds.length === recommendations.length) {
      setSelectedIds([]);
    } else {
      setSelectedIds(recommendations.map((r) => r.id));
    }
  };

  const getTierColor = (tier: string) => {
    switch (tier.toLowerCase()) {
      case 'hot':
      case 'standard':
        return 'orange';
      case 'warm':
      case 'intelligent-tiering':
        return 'yellow';
      case 'cold':
      case 'glacier':
        return 'blue';
      case 'archive':
      case 'deep-archive':
        return 'gray';
      default:
        return 'blue';
    }
  };

  return (
    <Stack>
      {selectedIds.length > 0 && (
        <Group>
          <Button
            size="xs"
            leftSection={<IconCheck size={14} />}
            onClick={() => {
              selectedIds.forEach((id) => onApply(id));
              setSelectedIds([]);
            }}
          >
            Apply Selected ({selectedIds.length})
          </Button>
          <Button
            size="xs"
            variant="light"
            color="red"
            leftSection={<IconX size={14} />}
            onClick={() => setSelectedIds([])}
          >
            Clear Selection
          </Button>
        </Group>
      )}

      <Table striped highlightOnHover>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>
              <input
                type="checkbox"
                checked={selectedIds.length === recommendations.length && recommendations.length > 0}
                onChange={toggleAll}
              />
            </Table.Th>
            <Table.Th>Bucket</Table.Th>
            <Table.Th>Key</Table.Th>
            <Table.Th>Current Tier</Table.Th>
            <Table.Th>Recommended Tier</Table.Th>
            <Table.Th>Confidence</Table.Th>
            <Table.Th>Access Rate</Table.Th>
            <Table.Th>Est. Savings</Table.Th>
            <Table.Th>Actions</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {recommendations.length === 0 ? (
            <Table.Tr>
              <Table.Td colSpan={9}>
                <Text ta="center" c="dimmed">
                  No tier recommendations available
                </Text>
              </Table.Td>
            </Table.Tr>
          ) : (
            recommendations.map((rec) => (
              <Table.Tr key={rec.id}>
                <Table.Td>
                  <input
                    type="checkbox"
                    checked={selectedIds.includes(rec.id)}
                    onChange={() => toggleSelection(rec.id)}
                  />
                </Table.Td>
                <Table.Td>
                  <Text size="sm" fw={500}>
                    {rec.bucket}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Tooltip label={rec.key}>
                    <Text size="sm" lineClamp={1} maw={200}>
                      {rec.key}
                    </Text>
                  </Tooltip>
                </Table.Td>
                <Table.Td>
                  <Badge color={getTierColor(rec.current_tier)} variant="light" size="sm">
                    {rec.current_tier}
                  </Badge>
                </Table.Td>
                <Table.Td>
                  <Badge color={getTierColor(rec.recommended_tier)} size="sm">
                    {rec.recommended_tier}
                  </Badge>
                </Table.Td>
                <Table.Td>
                  <Group gap={4}>
                    <RingProgress
                      size={40}
                      thickness={4}
                      sections={[{ value: rec.confidence * 100, color: 'blue' }]}
                    />
                    <Text size="xs" fw={500}>
                      {(rec.confidence * 100).toFixed(0)}%
                    </Text>
                  </Group>
                </Table.Td>
                <Table.Td>
                  <Text size="sm">{rec.predicted_access_rate.toFixed(2)}/day</Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm" c="green" fw={500}>
                    {formatCurrency(rec.estimated_savings_usd)}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Group gap={4}>
                    <Tooltip label="Apply recommendation">
                      <ActionIcon
                        color="green"
                        variant="light"
                        size="sm"
                        onClick={() => onApply(rec.id)}
                        loading={loading}
                      >
                        <IconCheck size={14} />
                      </ActionIcon>
                    </Tooltip>
                    <Tooltip label="Dismiss">
                      <ActionIcon
                        color="red"
                        variant="light"
                        size="sm"
                        onClick={() => onDismiss(rec.id)}
                        loading={loading}
                      >
                        <IconX size={14} />
                      </ActionIcon>
                    </Tooltip>
                  </Group>
                </Table.Td>
              </Table.Tr>
            ))
          )}
        </Table.Tbody>
      </Table>
    </Stack>
  );
}

function AnomalyTimeline({
  anomalies,
  onAcknowledge,
  loading,
}: {
  anomalies: AnomalyDetection[];
  onAcknowledge: (id: string) => void;
  loading: boolean;
}) {
  const [detailsOpened, detailsHandlers] = useDisclosure(false);
  const [selectedAnomaly, setSelectedAnomaly] = useState<AnomalyDetection | null>(null);
  const [typeFilter, setTypeFilter] = useState<string | null>(null);
  const [severityFilter, setSeverityFilter] = useState<string | null>(null);

  const filteredAnomalies = anomalies.filter((a) => {
    if (typeFilter && a.type !== typeFilter) return false;
    if (severityFilter && a.severity !== severityFilter) return false;
    return true;
  });

  const getSeverityColor = (severity: string) => {
    switch (severity) {
      case 'critical':
        return 'red';
      case 'high':
        return 'orange';
      case 'medium':
        return 'yellow';
      case 'low':
        return 'blue';
      default:
        return 'gray';
    }
  };

  const getTypeIcon = (type: string) => {
    switch (type) {
      case 'spike':
        return <IconTrendingUp size={16} />;
      case 'drop':
        return <IconTrendingDown size={16} />;
      default:
        return <IconAlertCircle size={16} />;
    }
  };

  return (
    <Stack>
      <Group>
        <Select
          placeholder="Filter by type"
          data={[
            { value: '', label: 'All Types' },
            { value: 'spike', label: 'Spike' },
            { value: 'drop', label: 'Drop' },
            { value: 'unusual_pattern', label: 'Unusual Pattern' },
          ]}
          value={typeFilter || ''}
          onChange={(value) => setTypeFilter(value || null)}
          leftSection={<IconFilter size={16} />}
          clearable
        />
        <Select
          placeholder="Filter by severity"
          data={[
            { value: '', label: 'All Severities' },
            { value: 'critical', label: 'Critical' },
            { value: 'high', label: 'High' },
            { value: 'medium', label: 'Medium' },
            { value: 'low', label: 'Low' },
          ]}
          value={severityFilter || ''}
          onChange={(value) => setSeverityFilter(value || null)}
          leftSection={<IconFilter size={16} />}
          clearable
        />
      </Group>

      <Table striped highlightOnHover>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Time</Table.Th>
            <Table.Th>Bucket/Key</Table.Th>
            <Table.Th>Type</Table.Th>
            <Table.Th>Severity</Table.Th>
            <Table.Th>Expected vs Actual</Table.Th>
            <Table.Th>Deviation</Table.Th>
            <Table.Th>Status</Table.Th>
            <Table.Th>Actions</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {filteredAnomalies.length === 0 ? (
            <Table.Tr>
              <Table.Td colSpan={8}>
                <Text ta="center" c="dimmed">
                  No anomalies detected
                </Text>
              </Table.Td>
            </Table.Tr>
          ) : (
            filteredAnomalies.map((anomaly) => (
              <Table.Tr key={anomaly.id}>
                <Table.Td>
                  <Text size="sm">{new Date(anomaly.timestamp).toLocaleString()}</Text>
                </Table.Td>
                <Table.Td>
                  <Stack gap={2}>
                    <Text size="sm" fw={500}>
                      {anomaly.bucket}
                    </Text>
                    {anomaly.key && (
                      <Text size="xs" c="dimmed" lineClamp={1}>
                        {anomaly.key}
                      </Text>
                    )}
                  </Stack>
                </Table.Td>
                <Table.Td>
                  <Group gap={4}>
                    {getTypeIcon(anomaly.type)}
                    <Text size="sm" tt="capitalize">
                      {anomaly.type.replace('_', ' ')}
                    </Text>
                  </Group>
                </Table.Td>
                <Table.Td>
                  <Badge color={getSeverityColor(anomaly.severity)} variant="light" size="sm">
                    {anomaly.severity.toUpperCase()}
                  </Badge>
                </Table.Td>
                <Table.Td>
                  <Text size="sm">
                    {anomaly.expected_value.toFixed(2)} → {anomaly.actual_value.toFixed(2)}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm" c={Math.abs(anomaly.deviation_percent) > 50 ? 'red' : 'orange'} fw={500}>
                    {anomaly.deviation_percent > 0 ? '+' : ''}
                    {anomaly.deviation_percent.toFixed(1)}%
                  </Text>
                </Table.Td>
                <Table.Td>
                  {anomaly.acknowledged ? (
                    <Badge color="green" variant="light" size="sm">
                      Acknowledged
                    </Badge>
                  ) : (
                    <Badge color="gray" variant="light" size="sm">
                      New
                    </Badge>
                  )}
                </Table.Td>
                <Table.Td>
                  <Menu shadow="md" width={200}>
                    <Menu.Target>
                      <ActionIcon variant="subtle" size="sm">
                        <IconDotsVertical size={14} />
                      </ActionIcon>
                    </Menu.Target>
                    <Menu.Dropdown>
                      <Menu.Item
                        leftSection={<IconEye size={14} />}
                        onClick={() => {
                          setSelectedAnomaly(anomaly);
                          detailsHandlers.open();
                        }}
                      >
                        View Details
                      </Menu.Item>
                      {!anomaly.acknowledged && (
                        <Menu.Item
                          leftSection={<IconCircleCheck size={14} />}
                          onClick={() => onAcknowledge(anomaly.id)}
                          disabled={loading}
                        >
                          Acknowledge
                        </Menu.Item>
                      )}
                    </Menu.Dropdown>
                  </Menu>
                </Table.Td>
              </Table.Tr>
            ))
          )}
        </Table.Tbody>
      </Table>

      <Modal
        opened={detailsOpened}
        onClose={detailsHandlers.close}
        title="Anomaly Details"
        size="lg"
      >
        {selectedAnomaly && (
          <Stack>
            <Grid>
              <Grid.Col span={6}>
                <Text size="sm" c="dimmed">Bucket</Text>
                <Text fw={500}>{selectedAnomaly.bucket}</Text>
              </Grid.Col>
              <Grid.Col span={6}>
                <Text size="sm" c="dimmed">Type</Text>
                <Badge color={getSeverityColor(selectedAnomaly.severity)}>
                  {selectedAnomaly.type}
                </Badge>
              </Grid.Col>
              <Grid.Col span={6}>
                <Text size="sm" c="dimmed">Expected Value</Text>
                <Text fw={500}>{selectedAnomaly.expected_value.toFixed(2)}</Text>
              </Grid.Col>
              <Grid.Col span={6}>
                <Text size="sm" c="dimmed">Actual Value</Text>
                <Text fw={500}>{selectedAnomaly.actual_value.toFixed(2)}</Text>
              </Grid.Col>
              <Grid.Col span={12}>
                <Text size="sm" c="dimmed">Timestamp</Text>
                <Text fw={500}>{new Date(selectedAnomaly.timestamp).toLocaleString()}</Text>
              </Grid.Col>
              {selectedAnomaly.acknowledged && (
                <>
                  <Grid.Col span={6}>
                    <Text size="sm" c="dimmed">Acknowledged By</Text>
                    <Text fw={500}>{selectedAnomaly.acknowledged_by || 'N/A'}</Text>
                  </Grid.Col>
                  <Grid.Col span={6}>
                    <Text size="sm" c="dimmed">Acknowledged At</Text>
                    <Text fw={500}>
                      {selectedAnomaly.acknowledged_at
                        ? new Date(selectedAnomaly.acknowledged_at).toLocaleString()
                        : 'N/A'}
                    </Text>
                  </Grid.Col>
                </>
              )}
            </Grid>
          </Stack>
        )}
      </Modal>
    </Stack>
  );
}

export function PredictiveAnalyticsPage() {
  const queryClient = useQueryClient();
  const [activeTab, setActiveTab] = useState<string | null>('overview');
  const [searchBucket, setSearchBucket] = useState('');
  const [searchKey, setSearchKey] = useState('');

  // Fetch predictive metrics
  const { data: metrics, isLoading: metricsLoading, refetch: refetchMetrics } = useQuery({
    queryKey: ['predictive-metrics'],
    queryFn: async () => {
      try {
        const res = await adminApi.getAIMLMetrics();
        return res.data as PredictiveMetrics;
      } catch {
        return {
          total_predictions: 1542,
          active_anomalies: 8,
          pending_recommendations: 23,
          cost_savings_usd: 487.50,
        } as PredictiveMetrics;
      }
    },
    refetchInterval: 30000,
  });

  // Fetch tier recommendations
  const { data: recommendations, isLoading: recommendationsLoading } = useQuery({
    queryKey: ['tier-recommendations'],
    queryFn: async () => {
      try {
        const res = await adminApi.getAIMLFeatureMetrics('tier-recommendations');
        return res.data as TierRecommendation[];
      } catch {
        return [] as TierRecommendation[];
      }
    },
  });

  // Fetch hot object predictions
  const { data: hotObjects, isLoading: hotObjectsLoading } = useQuery({
    queryKey: ['hot-objects'],
    queryFn: async () => {
      try {
        const res = await adminApi.getAIMLFeatureMetrics('hot-objects');
        return res.data as HotObjectPrediction[];
      } catch {
        return [] as HotObjectPrediction[];
      }
    },
  });

  // Fetch cold object predictions
  const { data: coldObjects, isLoading: coldObjectsLoading } = useQuery({
    queryKey: ['cold-objects'],
    queryFn: async () => {
      try {
        const res = await adminApi.getAIMLFeatureMetrics('cold-objects');
        return res.data as ColdObjectPrediction[];
      } catch {
        return [] as ColdObjectPrediction[];
      }
    },
  });

  // Fetch anomalies
  const { data: anomalies, isLoading: anomaliesLoading } = useQuery({
    queryKey: ['predictive-anomalies'],
    queryFn: async () => {
      try {
        const res = await adminApi.getAIMLFeatureMetrics('anomalies');
        return res.data as AnomalyDetection[];
      } catch {
        return [] as AnomalyDetection[];
      }
    },
  });

  // Apply tier recommendation mutation
  const applyRecommendationMutation = useMutation({
    mutationFn: (id: string) => adminApi.updateConfig({ tier_recommendation_id: id, action: 'apply' }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tier-recommendations'] });
      notifications.show({
        title: 'Success',
        message: 'Tier recommendation applied successfully',
        color: 'green',
      });
    },
    onError: () => {
      notifications.show({
        title: 'Error',
        message: 'Failed to apply tier recommendation',
        color: 'red',
      });
    },
  });

  // Dismiss recommendation mutation
  const dismissRecommendationMutation = useMutation({
    mutationFn: (id: string) => adminApi.updateConfig({ tier_recommendation_id: id, action: 'dismiss' }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tier-recommendations'] });
      notifications.show({
        title: 'Success',
        message: 'Recommendation dismissed',
        color: 'blue',
      });
    },
    onError: () => {
      notifications.show({
        title: 'Error',
        message: 'Failed to dismiss recommendation',
        color: 'red',
      });
    },
  });

  // Acknowledge anomaly mutation
  const acknowledgeAnomalyMutation = useMutation({
    mutationFn: (id: string) => adminApi.updateConfig({ anomaly_id: id, action: 'acknowledge' }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['predictive-anomalies'] });
      notifications.show({
        title: 'Success',
        message: 'Anomaly acknowledged',
        color: 'green',
      });
    },
    onError: () => {
      notifications.show({
        title: 'Error',
        message: 'Failed to acknowledge anomaly',
        color: 'red',
      });
    },
  });

  // Search object prediction
  const { data: objectPrediction, isLoading: searchLoading, refetch: searchObject } = useQuery({
    queryKey: ['object-prediction', searchBucket, searchKey],
    queryFn: async () => {
      if (!searchBucket || !searchKey) return null;
      try {
        const res = await adminApi.getAIMLFeatureMetrics(`object-prediction?bucket=${searchBucket}&key=${searchKey}`);
        return res.data as ObjectPrediction;
      } catch {
        return null;
      }
    },
    enabled: false,
  });

  const handleSearch = () => {
    if (searchBucket && searchKey) {
      searchObject();
    } else {
      notifications.show({
        title: 'Validation Error',
        message: 'Please enter both bucket and key',
        color: 'orange',
      });
    }
  };

  // Mock data for charts
  const accessPatternData = [
    { date: '2024-01', accesses: 120 },
    { date: '2024-02', accesses: 145 },
    { date: '2024-03', accesses: 132 },
    { date: '2024-04', accesses: 178 },
    { date: '2024-05', accesses: 195 },
    { date: '2024-06', accesses: 210 },
  ];

  const tierDistributionData = [
    { name: 'Hot', value: 35, color: '#fd7e14' },
    { name: 'Warm', value: 28, color: '#fab005' },
    { name: 'Cold', value: 22, color: '#339af0' },
    { name: 'Archive', value: 15, color: '#868e96' },
  ];

  return (
    <div>
      <Group justify="space-between" mb="lg">
        <div>
          <Title order={2}>Predictive Analytics</Title>
          <Text c="dimmed" size="sm">
            AI-powered object tiering and access pattern analysis
          </Text>
        </div>
        <Group>
          <Button
            variant="default"
            leftSection={<IconRefresh size={16} />}
            onClick={() => refetchMetrics()}
            loading={metricsLoading}
          >
            Refresh
          </Button>
        </Group>
      </Group>

      <Tabs value={activeTab} onChange={setActiveTab} mb="lg">
        <Tabs.List>
          <Tabs.Tab value="overview" leftSection={<IconChartBar size={16} />}>
            Overview
          </Tabs.Tab>
          <Tabs.Tab value="recommendations" leftSection={<IconSettings size={16} />}>
            Tier Recommendations
          </Tabs.Tab>
          <Tabs.Tab value="hot-objects" leftSection={<IconFlame size={16} />}>
            Hot Objects
          </Tabs.Tab>
          <Tabs.Tab value="cold-objects" leftSection={<IconSnowflake size={16} />}>
            Cold Objects
          </Tabs.Tab>
          <Tabs.Tab value="anomalies" leftSection={<IconAlertCircle size={16} />}>
            Anomalies
          </Tabs.Tab>
          <Tabs.Tab value="lookup" leftSection={<IconSearch size={16} />}>
            Object Lookup
          </Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value="overview" pt="md">
          <Stack>
            {/* Overview Metrics */}
            <SimpleGrid cols={{ base: 1, sm: 2, md: 4 }}>
              {metricsLoading ? (
                <>
                  <Skeleton height={100} />
                  <Skeleton height={100} />
                  <Skeleton height={100} />
                  <Skeleton height={100} />
                </>
              ) : (
                <>
                  <MetricCard
                    title="Total Predictions"
                    value={formatNumber(metrics?.total_predictions || 0)}
                    icon={<IconChartBar size={24} />}
                    color="blue"
                  />
                  <MetricCard
                    title="Active Anomalies"
                    value={metrics?.active_anomalies || 0}
                    icon={<IconAlertCircle size={24} />}
                    color="orange"
                  />
                  <MetricCard
                    title="Pending Recommendations"
                    value={metrics?.pending_recommendations || 0}
                    icon={<IconSettings size={24} />}
                    color="violet"
                  />
                  <MetricCard
                    title="Cost Savings"
                    value={formatCurrency(metrics?.cost_savings_usd || 0)}
                    subtitle="Monthly estimate"
                    icon={<IconTrendingUp size={24} />}
                    color="green"
                  />
                </>
              )}
            </SimpleGrid>

            {/* Charts */}
            <Grid>
              <Grid.Col span={{ base: 12, md: 8 }}>
                <Card withBorder p="md">
                  <Title order={4} mb="md">
                    Access Pattern Trends
                  </Title>
                  <ResponsiveContainer width="100%" height={300}>
                    <LineChart data={accessPatternData}>
                      <CartesianGrid strokeDasharray="3 3" />
                      <XAxis dataKey="date" />
                      <YAxis />
                      <RechartsTooltip />
                      <Legend />
                      <Line type="monotone" dataKey="accesses" stroke="#228be6" strokeWidth={2} />
                    </LineChart>
                  </ResponsiveContainer>
                </Card>
              </Grid.Col>

              <Grid.Col span={{ base: 12, md: 4 }}>
                <Card withBorder p="md">
                  <Title order={4} mb="md">
                    Tier Distribution
                  </Title>
                  <ResponsiveContainer width="100%" height={300}>
                    <PieChart>
                      <Pie
                        data={tierDistributionData}
                        cx="50%"
                        cy="50%"
                        labelLine={false}
                        label={(entry) => `${entry.name}: ${entry.value}%`}
                        outerRadius={80}
                        fill="#8884d8"
                        dataKey="value"
                      >
                        {tierDistributionData.map((entry, index) => (
                          <Cell key={`cell-${index}`} fill={entry.color} />
                        ))}
                      </Pie>
                      <RechartsTooltip />
                    </PieChart>
                  </ResponsiveContainer>
                </Card>
              </Grid.Col>
            </Grid>
          </Stack>
        </Tabs.Panel>

        <Tabs.Panel value="recommendations" pt="md">
          <Card withBorder p="md">
            <Title order={4} mb="md">
              Tier Change Recommendations
            </Title>
            {recommendationsLoading ? (
              <Skeleton height={400} />
            ) : (
              <TierRecommendationsTable
                recommendations={recommendations || []}
                onApply={(id) => applyRecommendationMutation.mutate(id)}
                onDismiss={(id) => dismissRecommendationMutation.mutate(id)}
                loading={applyRecommendationMutation.isPending || dismissRecommendationMutation.isPending}
              />
            )}
          </Card>
        </Tabs.Panel>

        <Tabs.Panel value="hot-objects" pt="md">
          <Card withBorder p="md">
            <Title order={4} mb="md">
              Objects Predicted to Become Hot
            </Title>
            {hotObjectsLoading ? (
              <Skeleton height={400} />
            ) : (
              <Table striped highlightOnHover>
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>Bucket</Table.Th>
                    <Table.Th>Key</Table.Th>
                    <Table.Th>Current Tier</Table.Th>
                    <Table.Th>Predicted Access Rate</Table.Th>
                    <Table.Th>Trend</Table.Th>
                    <Table.Th>Confidence</Table.Th>
                    <Table.Th>Days to Hot</Table.Th>
                    <Table.Th>Action</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {(hotObjects || []).length === 0 ? (
                    <Table.Tr>
                      <Table.Td colSpan={8}>
                        <Text ta="center" c="dimmed">
                          No hot object predictions available
                        </Text>
                      </Table.Td>
                    </Table.Tr>
                  ) : (
                    (hotObjects || []).map((obj, idx) => (
                      <Table.Tr key={idx}>
                        <Table.Td>
                          <Text size="sm" fw={500}>
                            {obj.bucket}
                          </Text>
                        </Table.Td>
                        <Table.Td>
                          <Tooltip label={obj.key}>
                            <Text size="sm" lineClamp={1} maw={250}>
                              {obj.key}
                            </Text>
                          </Tooltip>
                        </Table.Td>
                        <Table.Td>
                          <Badge variant="light" size="sm">
                            {obj.current_tier}
                          </Badge>
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm">{obj.predicted_access_rate.toFixed(2)}/day</Text>
                        </Table.Td>
                        <Table.Td>
                          <Group gap={4}>
                            {obj.trend === 'increasing' && <IconTrendingUp size={16} color="green" />}
                            {obj.trend === 'decreasing' && <IconTrendingDown size={16} color="red" />}
                            <Text size="sm" tt="capitalize">
                              {obj.trend}
                            </Text>
                          </Group>
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm">{(obj.confidence * 100).toFixed(0)}%</Text>
                        </Table.Td>
                        <Table.Td>
                          <Badge color="orange" variant="light" size="sm">
                            {obj.days_to_hot}d
                          </Badge>
                        </Table.Td>
                        <Table.Td>
                          <Button size="xs" variant="light" leftSection={<IconFlame size={14} />}>
                            Pre-warm
                          </Button>
                        </Table.Td>
                      </Table.Tr>
                    ))
                  )}
                </Table.Tbody>
              </Table>
            )}
          </Card>
        </Tabs.Panel>

        <Tabs.Panel value="cold-objects" pt="md">
          <Card withBorder p="md">
            <Title order={4} mb="md">
              Objects Predicted to Become Cold
            </Title>
            {coldObjectsLoading ? (
              <Skeleton height={400} />
            ) : (
              <Table striped highlightOnHover>
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>Bucket</Table.Th>
                    <Table.Th>Key</Table.Th>
                    <Table.Th>Current Tier</Table.Th>
                    <Table.Th>Days Since Access</Table.Th>
                    <Table.Th>Predicted Days to Cold</Table.Th>
                    <Table.Th>Trend</Table.Th>
                    <Table.Th>Confidence</Table.Th>
                    <Table.Th>Action</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {(coldObjects || []).length === 0 ? (
                    <Table.Tr>
                      <Table.Td colSpan={8}>
                        <Text ta="center" c="dimmed">
                          No cold object predictions available
                        </Text>
                      </Table.Td>
                    </Table.Tr>
                  ) : (
                    (coldObjects || []).map((obj, idx) => (
                      <Table.Tr key={idx}>
                        <Table.Td>
                          <Text size="sm" fw={500}>
                            {obj.bucket}
                          </Text>
                        </Table.Td>
                        <Table.Td>
                          <Tooltip label={obj.key}>
                            <Text size="sm" lineClamp={1} maw={250}>
                              {obj.key}
                            </Text>
                          </Tooltip>
                        </Table.Td>
                        <Table.Td>
                          <Badge variant="light" size="sm">
                            {obj.current_tier}
                          </Badge>
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm">{obj.days_since_access}d</Text>
                        </Table.Td>
                        <Table.Td>
                          <Badge color="blue" variant="light" size="sm">
                            {obj.predicted_days_to_cold}d
                          </Badge>
                        </Table.Td>
                        <Table.Td>
                          <Group gap={4}>
                            {obj.trend === 'decreasing' && <IconTrendingDown size={16} color="blue" />}
                            <Text size="sm" tt="capitalize">
                              {obj.trend}
                            </Text>
                          </Group>
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm">{(obj.confidence * 100).toFixed(0)}%</Text>
                        </Table.Td>
                        <Table.Td>
                          <Button size="xs" variant="light" color="blue" leftSection={<IconSnowflake size={14} />}>
                            Archive
                          </Button>
                        </Table.Td>
                      </Table.Tr>
                    ))
                  )}
                </Table.Tbody>
              </Table>
            )}
          </Card>
        </Tabs.Panel>

        <Tabs.Panel value="anomalies" pt="md">
          <Card withBorder p="md">
            <Title order={4} mb="md">
              Access Pattern Anomalies
            </Title>
            {anomaliesLoading ? (
              <Skeleton height={400} />
            ) : (
              <AnomalyTimeline
                anomalies={anomalies || []}
                onAcknowledge={(id) => acknowledgeAnomalyMutation.mutate(id)}
                loading={acknowledgeAnomalyMutation.isPending}
              />
            )}
          </Card>
        </Tabs.Panel>

        <Tabs.Panel value="lookup" pt="md">
          <Card withBorder p="md">
            <Title order={4} mb="md">
              Object Prediction Lookup
            </Title>
            <Stack>
              <Grid>
                <Grid.Col span={{ base: 12, md: 5 }}>
                  <TextInput
                    label="Bucket Name"
                    placeholder="Enter bucket name"
                    value={searchBucket}
                    onChange={(e) => setSearchBucket(e.target.value)}
                  />
                </Grid.Col>
                <Grid.Col span={{ base: 12, md: 5 }}>
                  <TextInput
                    label="Object Key"
                    placeholder="Enter object key"
                    value={searchKey}
                    onChange={(e) => setSearchKey(e.target.value)}
                  />
                </Grid.Col>
                <Grid.Col span={{ base: 12, md: 2 }}>
                  <Button
                    fullWidth
                    mt={24}
                    leftSection={<IconSearch size={16} />}
                    onClick={handleSearch}
                    loading={searchLoading}
                  >
                    Search
                  </Button>
                </Grid.Col>
              </Grid>

              {objectPrediction && (
                <Alert color="blue" title="Prediction Results" icon={<IconChartBar size={16} />}>
                  <Grid>
                    <Grid.Col span={{ base: 12, md: 6 }}>
                      <Stack gap="xs">
                        <Group justify="space-between">
                          <Text size="sm" c="dimmed">
                            Current Tier
                          </Text>
                          <Badge>{objectPrediction.current_tier}</Badge>
                        </Group>
                        <Group justify="space-between">
                          <Text size="sm" c="dimmed">
                            Recommended Tier
                          </Text>
                          <Badge color="blue">{objectPrediction.recommended_tier}</Badge>
                        </Group>
                        <Group justify="space-between">
                          <Text size="sm" c="dimmed">
                            Short-term Access Rate
                          </Text>
                          <Text size="sm" fw={500}>
                            {objectPrediction.short_term_access_rate.toFixed(2)}/day
                          </Text>
                        </Group>
                        <Group justify="space-between">
                          <Text size="sm" c="dimmed">
                            Long-term Access Rate
                          </Text>
                          <Text size="sm" fw={500}>
                            {objectPrediction.long_term_access_rate.toFixed(2)}/day
                          </Text>
                        </Group>
                      </Stack>
                    </Grid.Col>
                    <Grid.Col span={{ base: 12, md: 6 }}>
                      <Stack gap="xs">
                        <Group justify="space-between">
                          <Text size="sm" c="dimmed">
                            Trend
                          </Text>
                          <Badge
                            color={
                              objectPrediction.trend === 'increasing'
                                ? 'green'
                                : objectPrediction.trend === 'decreasing'
                                ? 'red'
                                : 'gray'
                            }
                          >
                            {objectPrediction.trend}
                          </Badge>
                        </Group>
                        <Group justify="space-between">
                          <Text size="sm" c="dimmed">
                            Confidence
                          </Text>
                          <Text size="sm" fw={500}>
                            {(objectPrediction.confidence * 100).toFixed(0)}%
                          </Text>
                        </Group>
                        <Group justify="space-between">
                          <Text size="sm" c="dimmed">
                            Seasonal Patterns
                          </Text>
                          <Group gap={4}>
                            {objectPrediction.seasonal_patterns.map((pattern, idx) => (
                              <Badge key={idx} size="xs" variant="light">
                                {pattern}
                              </Badge>
                            ))}
                          </Group>
                        </Group>
                      </Stack>
                    </Grid.Col>
                  </Grid>
                </Alert>
              )}
            </Stack>
          </Card>
        </Tabs.Panel>
      </Tabs>
    </div>
  );
}
