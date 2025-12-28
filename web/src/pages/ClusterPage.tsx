import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  Title,
  Card,
  Grid,
  Text,
  Group,
  Badge,
  Table,
  Paper,
  Skeleton,
  Progress,
  ThemeIcon,
  RingProgress,
  Select,
  Stack,
  Code,
  Tooltip,
} from '@mantine/core';
import {
  IconServer,
  IconCheck,
  IconX,
  IconClock,
  IconRefresh,
  IconHeartbeat,
  IconDatabase,
  IconFiles,
  IconNetwork,
} from '@tabler/icons-react';
import { adminApi } from '../api/client';

interface NodeInfo {
  node_id: string;
  raft_addr: string;
  s3_addr: string;
  admin_addr: string;
  gossip_addr?: string;
  role: string;
  version?: string;
  status: string;
  is_leader: boolean;
  is_voter: boolean;
  joined_at?: string;
  last_seen?: string;
  storage_info?: {
    total_bytes: number;
    used_bytes: number;
    available_bytes: number;
    object_count: number;
  };
}

interface ClusterInfo {
  cluster_id: string;
  leader_id: string;
  leader_address: string;
  nodes: NodeInfo[];
  raft_state: string;
}

interface ListNodesResponse {
  nodes: NodeInfo[];
  total_nodes: number;
  healthy_nodes: number;
  leader_id: string;
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

export function ClusterPage() {
  const [refreshInterval, setRefreshInterval] = useState(5000);

  const { data: clusterStatus, isLoading: clusterLoading } = useQuery({
    queryKey: ['cluster-status'],
    queryFn: () => adminApi.getClusterStatus().then((res) => res.data as ClusterInfo),
    refetchInterval: refreshInterval,
  });

  const { data: nodesResponse, isLoading: nodesLoading } = useQuery({
    queryKey: ['cluster-nodes'],
    queryFn: () => adminApi.listNodes().then((res) => res.data as ListNodesResponse),
    refetchInterval: refreshInterval,
  });

  // Extract nodes array from response
  const nodes = nodesResponse?.nodes || [];

  const { data: storageInfo, isLoading: storageLoading } = useQuery({
    queryKey: ['storage-info'],
    queryFn: () => adminApi.getStorageInfo().then((res) => res.data),
    refetchInterval: refreshInterval,
  });

  const { data: raftState, isLoading: raftLoading } = useQuery({
    queryKey: ['raft-state'],
    queryFn: () => adminApi.getRaftState().then((res) => res.data),
    refetchInterval: refreshInterval,
  });

  const isLoading = clusterLoading || nodesLoading || storageLoading;

  const getStatusColor = (status: string): string => {
    switch (status?.toLowerCase()) {
      case 'healthy':
      case 'alive':
      case 'leader':
      case 'follower':
        return 'green';
      case 'candidate':
        return 'yellow';
      case 'unhealthy':
      case 'down':
      case 'dead':
        return 'red';
      default:
        return 'gray';
    }
  };

  const getRaftStateColor = (state: string): string => {
    switch (state?.toLowerCase()) {
      case 'leader':
        return 'blue';
      case 'follower':
        return 'green';
      case 'candidate':
        return 'yellow';
      default:
        return 'gray';
    }
  };

  // Calculate total storage stats
  const totalStorage = nodes?.reduce(
    (acc, node) => {
      if (node.storage_info) {
        acc.total += node.storage_info.total_bytes;
        acc.used += node.storage_info.used_bytes;
        acc.objects += node.storage_info.object_count;
      }
      return acc;
    },
    { total: 0, used: 0, objects: 0 }
  ) || { total: 0, used: 0, objects: 0 };

  const storageUsagePercent = totalStorage.total > 0
    ? Math.round((totalStorage.used / totalStorage.total) * 100)
    : 0;

  const healthyNodes = nodes?.filter((n) => n.status === 'healthy' || n.status === 'alive').length || 0;
  const totalNodes = nodes?.length || 1;

  return (
    <div>
      <Group justify="space-between" mb="lg">
        <Title order={2}>Cluster Management</Title>
        <Group>
          <Select
            value={String(refreshInterval)}
            onChange={(value) => setRefreshInterval(Number(value))}
            data={[
              { value: '2000', label: '2s refresh' },
              { value: '5000', label: '5s refresh' },
              { value: '10000', label: '10s refresh' },
              { value: '30000', label: '30s refresh' },
            ]}
            size="xs"
            w={120}
          />
          <Badge color="green" variant="light" leftSection={<IconRefresh size={12} />}>
            Live
          </Badge>
        </Group>
      </Group>

      {/* Status Cards */}
      <Grid mb="lg">
        {/* Cluster Health */}
        <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
          <Card withBorder radius="md" p="lg" h="100%">
            <Group justify="space-between" mb="xs">
              <Text c="dimmed" size="xs" tt="uppercase" fw={700}>
                Cluster Health
              </Text>
              <ThemeIcon color={healthyNodes === totalNodes ? 'green' : 'yellow'} variant="light" size="sm">
                <IconHeartbeat size={14} />
              </ThemeIcon>
            </Group>
            {isLoading ? (
              <Skeleton height={60} />
            ) : (
              <>
                <Text fw={700} size="xl">
                  {healthyNodes} / {totalNodes}
                </Text>
                <Text size="xs" c="dimmed">
                  nodes healthy
                </Text>
                <Progress
                  value={(healthyNodes / totalNodes) * 100}
                  color={healthyNodes === totalNodes ? 'green' : 'yellow'}
                  size="sm"
                  mt="md"
                />
              </>
            )}
          </Card>
        </Grid.Col>

        {/* Storage Usage */}
        <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
          <Card withBorder radius="md" p="lg" h="100%">
            <Group justify="space-between" mb="xs">
              <Text c="dimmed" size="xs" tt="uppercase" fw={700}>
                Storage Used
              </Text>
              <ThemeIcon
                color={storageUsagePercent > 90 ? 'red' : storageUsagePercent > 70 ? 'yellow' : 'blue'}
                variant="light"
                size="sm"
              >
                <IconDatabase size={14} />
              </ThemeIcon>
            </Group>
            {storageLoading ? (
              <Skeleton height={60} />
            ) : (
              <>
                <Text fw={700} size="xl">
                  {storageUsagePercent}%
                </Text>
                <Text size="xs" c="dimmed">
                  {formatBytes(totalStorage.used)} of {formatBytes(totalStorage.total)}
                </Text>
                <Progress
                  value={storageUsagePercent}
                  color={storageUsagePercent > 90 ? 'red' : storageUsagePercent > 70 ? 'yellow' : 'blue'}
                  size="sm"
                  mt="md"
                />
              </>
            )}
          </Card>
        </Grid.Col>

        {/* Object Count */}
        <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
          <Card withBorder radius="md" p="lg" h="100%">
            <Group justify="space-between" mb="xs">
              <Text c="dimmed" size="xs" tt="uppercase" fw={700}>
                Total Objects
              </Text>
              <ThemeIcon color="violet" variant="light" size="sm">
                <IconFiles size={14} />
              </ThemeIcon>
            </Group>
            {storageLoading ? (
              <Skeleton height={60} />
            ) : (
              <>
                <Text fw={700} size="xl">
                  {totalStorage.objects.toLocaleString()}
                </Text>
                <Text size="xs" c="dimmed">
                  objects stored
                </Text>
              </>
            )}
          </Card>
        </Grid.Col>

        {/* Raft State */}
        <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
          <Card withBorder radius="md" p="lg" h="100%">
            <Group justify="space-between" mb="xs">
              <Text c="dimmed" size="xs" tt="uppercase" fw={700}>
                Raft State
              </Text>
              <ThemeIcon color={getRaftStateColor(clusterStatus?.raft_state || '')} variant="light" size="sm">
                <IconNetwork size={14} />
              </ThemeIcon>
            </Group>
            {clusterLoading ? (
              <Skeleton height={60} />
            ) : (
              <>
                <Badge size="lg" color={getRaftStateColor(clusterStatus?.raft_state || '')} variant="light">
                  {clusterStatus?.raft_state || 'Unknown'}
                </Badge>
                <Text size="xs" c="dimmed" mt="xs">
                  {storageInfo?.is_leader ? 'This node is the leader' : 'Following the leader'}
                </Text>
              </>
            )}
          </Card>
        </Grid.Col>
      </Grid>

      {/* Cluster Details */}
      <Grid mb="lg">
        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder radius="md" p="lg" h="100%">
            <Text fw={500} size="lg" mb="md">
              Cluster Information
            </Text>
            {clusterLoading ? (
              <Skeleton height={150} />
            ) : (
              <Stack gap="sm">
                <Group>
                  <Text c="dimmed" size="sm" w={140}>
                    Cluster ID:
                  </Text>
                  <Code style={{ wordBreak: 'break-all', fontSize: '12px' }}>
                    {clusterStatus?.cluster_id || 'N/A'}
                  </Code>
                </Group>
                <Group>
                  <Text c="dimmed" size="sm" w={140}>
                    Leader Address:
                  </Text>
                  <Text size="sm" fw={500}>
                    {clusterStatus?.leader_address || 'N/A'}
                  </Text>
                </Group>
                <Group>
                  <Text c="dimmed" size="sm" w={140}>
                    Total Nodes:
                  </Text>
                  <Badge variant="light">{totalNodes}</Badge>
                </Group>
                <Group>
                  <Text c="dimmed" size="sm" w={140}>
                    Healthy Nodes:
                  </Text>
                  <Badge color="green" variant="light">
                    {healthyNodes}
                  </Badge>
                </Group>
              </Stack>
            )}
          </Card>
        </Grid.Col>

        {/* Raft State Details */}
        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder radius="md" p="lg" h="100%">
            <Text fw={500} size="lg" mb="md">
              Raft Consensus State
            </Text>
            {raftLoading ? (
              <Skeleton height={150} />
            ) : (
              <Stack gap="sm">
                <Group>
                  <Text c="dimmed" size="sm" w={140}>
                    Current Term:
                  </Text>
                  <Badge variant="light">{raftState?.term || 0}</Badge>
                </Group>
                <Group>
                  <Text c="dimmed" size="sm" w={140}>
                    Commit Index:
                  </Text>
                  <Text size="sm">{raftState?.commit_index || 0}</Text>
                </Group>
                <Group>
                  <Text c="dimmed" size="sm" w={140}>
                    Applied Index:
                  </Text>
                  <Text size="sm">{raftState?.applied_index || 0}</Text>
                </Group>
                <Group>
                  <Text c="dimmed" size="sm" w={140}>
                    Voters:
                  </Text>
                  <Badge variant="light">{raftState?.voters || 1}</Badge>
                </Group>
              </Stack>
            )}
          </Card>
        </Grid.Col>
      </Grid>

      {/* Storage Visualization */}
      <Card withBorder radius="md" p="lg" mb="lg">
        <Text fw={500} size="lg" mb="md">
          Storage Distribution
        </Text>
        {storageLoading ? (
          <Skeleton height={100} />
        ) : nodes?.length ? (
          <Group justify="center" gap="xl">
            {nodes.map((node) => {
              const nodeUsage = node.storage_info
                ? (node.storage_info.used_bytes / node.storage_info.total_bytes) * 100
                : 0;
              return (
                <div key={node.node_id} style={{ textAlign: 'center' }}>
                  <RingProgress
                    sections={[{ value: nodeUsage, color: nodeUsage > 90 ? 'red' : nodeUsage > 70 ? 'yellow' : 'blue' }]}
                    label={
                      <Text ta="center" size="sm" fw={700}>
                        {Math.round(nodeUsage)}%
                      </Text>
                    }
                    size={100}
                    thickness={8}
                  />
                  <Text size="sm" fw={500} mt="xs">
                    {node.node_id.slice(0, 8)}
                  </Text>
                  <Text size="xs" c="dimmed">
                    {node.storage_info
                      ? `${formatBytes(node.storage_info.used_bytes)} / ${formatBytes(node.storage_info.total_bytes)}`
                      : 'N/A'}
                  </Text>
                </div>
              );
            })}
          </Group>
        ) : (
          <Text c="dimmed" ta="center">
            Single-node mode - no distribution data available
          </Text>
        )}
      </Card>

      {/* Nodes Table */}
      <Paper withBorder>
        <Group p="md" justify="space-between">
          <Text fw={500}>Cluster Nodes</Text>
          <Group gap="xs">
            <Badge variant="light" color="green">
              {healthyNodes} healthy
            </Badge>
            <Badge variant="light">{totalNodes} total</Badge>
          </Group>
        </Group>

        {nodesLoading ? (
          <Skeleton height={200} m="md" />
        ) : nodes?.length ? (
          <Table striped highlightOnHover>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Node</Table.Th>
                <Table.Th>Address</Table.Th>
                <Table.Th>Role</Table.Th>
                <Table.Th>Status</Table.Th>
                <Table.Th>Storage</Table.Th>
                <Table.Th>Objects</Table.Th>
                <Table.Th>Last Heartbeat</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {nodes.map((node: NodeInfo) => (
                <Table.Tr key={node.node_id}>
                  <Table.Td>
                    <Group gap="xs">
                      <ThemeIcon color="blue" variant="light" size="sm">
                        <IconServer size={14} />
                      </ThemeIcon>
                      <div>
                        <Text size="sm" fw={500}>
                          {node.node_id.slice(0, 8)}
                        </Text>
                        <Tooltip label={node.node_id}>
                          <Text size="xs" c="dimmed" style={{ cursor: 'pointer' }}>
                            {node.node_id.slice(0, 12)}...
                          </Text>
                        </Tooltip>
                      </div>
                    </Group>
                  </Table.Td>
                  <Table.Td>
                    <Code fz="sm">{node.admin_addr}</Code>
                  </Table.Td>
                  <Table.Td>
                    <Badge
                      variant="light"
                      color={node.role === 'leader' ? 'blue' : node.role === 'gateway' ? 'violet' : 'gray'}
                    >
                      {node.role || 'gateway'}
                    </Badge>
                  </Table.Td>
                  <Table.Td>
                    <Group gap="xs">
                      {node.status === 'healthy' || node.status === 'alive' ? (
                        <IconCheck size={16} color="var(--mantine-color-green-6)" />
                      ) : (
                        <IconX size={16} color="var(--mantine-color-red-6)" />
                      )}
                      <Badge color={getStatusColor(node.status)} variant="light">
                        {node.status || 'alive'}
                      </Badge>
                    </Group>
                  </Table.Td>
                  <Table.Td>
                    {node.storage_info ? (
                      <div>
                        <Text size="sm">
                          {formatBytes(node.storage_info.used_bytes)} /{' '}
                          {formatBytes(node.storage_info.total_bytes)}
                        </Text>
                        <Progress
                          value={
                            (node.storage_info.used_bytes / node.storage_info.total_bytes) * 100
                          }
                          size="xs"
                          mt={4}
                        />
                      </div>
                    ) : (
                      <Text size="sm" c="dimmed">
                        N/A
                      </Text>
                    )}
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">
                      {node.storage_info?.object_count?.toLocaleString() || 'N/A'}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Group gap="xs">
                      <IconClock size={14} />
                      <Text size="sm">
                        {node.last_seen
                          ? new Date(node.last_seen).toLocaleTimeString()
                          : 'N/A'}
                      </Text>
                    </Group>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        ) : (
          <div style={{ padding: '2rem', textAlign: 'center' }}>
            <Text c="dimmed" mb="md">
              Running in single-node mode
            </Text>
            <Text size="sm" c="dimmed">
              Add more nodes to enable high availability and distributed storage
            </Text>
          </div>
        )}
      </Paper>
    </div>
  );
}
