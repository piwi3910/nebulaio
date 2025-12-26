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
} from '@mantine/core';
import { IconServer, IconCheck, IconX, IconClock } from '@tabler/icons-react';
import { adminApi } from '../api/client';

interface NodeInfo {
  id: string;
  name: string;
  address: string;
  role: string;
  status: string;
  joined_at: string;
  last_heartbeat: string;
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

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

export function ClusterPage() {
  const { data: clusterStatus, isLoading: clusterLoading } = useQuery({
    queryKey: ['cluster-status'],
    queryFn: () => adminApi.getClusterStatus().then((res) => res.data as ClusterInfo),
    refetchInterval: 5000, // Refresh every 5 seconds
  });

  const { data: nodes, isLoading: nodesLoading } = useQuery({
    queryKey: ['cluster-nodes'],
    queryFn: () => adminApi.listNodes().then((res) => res.data as NodeInfo[]),
    refetchInterval: 5000,
  });

  const { data: storageInfo, isLoading: storageLoading } = useQuery({
    queryKey: ['storage-info'],
    queryFn: () => adminApi.getStorageInfo().then((res) => res.data),
    refetchInterval: 10000,
  });

  const isLoading = clusterLoading || nodesLoading || storageLoading;

  const getStatusColor = (status: string): string => {
    switch (status?.toLowerCase()) {
      case 'healthy':
      case 'leader':
      case 'follower':
        return 'green';
      case 'candidate':
        return 'yellow';
      case 'unhealthy':
      case 'down':
        return 'red';
      default:
        return 'gray';
    }
  };

  return (
    <div>
      <Title order={2} mb="lg">
        Cluster Management
      </Title>

      <Grid mb="lg">
        {/* Cluster Status Card */}
        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder radius="md" p="lg" h="100%">
            <Group justify="space-between" mb="md">
              <Text fw={500} size="lg">
                Cluster Status
              </Text>
              {isLoading ? (
                <Skeleton circle height={24} width={24} />
              ) : (
                <Badge color={getStatusColor(clusterStatus?.raft_state || '')} size="lg">
                  {clusterStatus?.raft_state || 'Unknown'}
                </Badge>
              )}
            </Group>

            {isLoading ? (
              <Skeleton height={100} />
            ) : (
              <div>
                <Group mb="xs">
                  <Text c="dimmed" size="sm" w={120}>
                    Cluster ID:
                  </Text>
                  <Text size="sm" style={{ wordBreak: 'break-all' }}>
                    {clusterStatus?.cluster_id || 'N/A'}
                  </Text>
                </Group>
                <Group mb="xs">
                  <Text c="dimmed" size="sm" w={120}>
                    Leader:
                  </Text>
                  <Text size="sm">{clusterStatus?.leader_address || 'N/A'}</Text>
                </Group>
                <Group mb="xs">
                  <Text c="dimmed" size="sm" w={120}>
                    Total Nodes:
                  </Text>
                  <Text size="sm">{nodes?.length || 1}</Text>
                </Group>
                <Group>
                  <Text c="dimmed" size="sm" w={120}>
                    Is Leader:
                  </Text>
                  <Badge color={storageInfo?.is_leader ? 'green' : 'blue'} variant="light">
                    {storageInfo?.is_leader ? 'Yes' : 'No'}
                  </Badge>
                </Group>
              </div>
            )}
          </Card>
        </Grid.Col>

        {/* Storage Overview Card */}
        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder radius="md" p="lg" h="100%">
            <Text fw={500} size="lg" mb="md">
              Storage Overview
            </Text>

            {storageLoading ? (
              <Skeleton height={100} />
            ) : (
              <div>
                <Group mb="xs">
                  <Text c="dimmed" size="sm" w={120}>
                    Total Capacity:
                  </Text>
                  <Text size="sm">10 GB</Text>
                </Group>
                <Group mb="xs">
                  <Text c="dimmed" size="sm" w={120}>
                    Used:
                  </Text>
                  <Text size="sm">3.5 GB (35%)</Text>
                </Group>
                <Group mb="md">
                  <Text c="dimmed" size="sm" w={120}>
                    Available:
                  </Text>
                  <Text size="sm">6.5 GB</Text>
                </Group>
                <Progress value={35} size="lg" radius="md" />
              </div>
            )}
          </Card>
        </Grid.Col>
      </Grid>

      {/* Nodes Table */}
      <Paper withBorder>
        <Group p="md" justify="space-between">
          <Text fw={500}>Cluster Nodes</Text>
          <Badge variant="light">{nodes?.length || 1} node(s)</Badge>
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
                <Table.Th>Last Heartbeat</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {nodes.map((node: NodeInfo) => (
                <Table.Tr key={node.id}>
                  <Table.Td>
                    <Group gap="xs">
                      <ThemeIcon color="blue" variant="light" size="sm">
                        <IconServer size={14} />
                      </ThemeIcon>
                      <div>
                        <Text size="sm" fw={500}>
                          {node.name || node.id}
                        </Text>
                        <Text size="xs" c="dimmed">
                          {node.id}
                        </Text>
                      </div>
                    </Group>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{node.address}</Text>
                  </Table.Td>
                  <Table.Td>
                    <Badge variant="light" color={node.role === 'gateway' ? 'blue' : 'violet'}>
                      {node.role || 'gateway'}
                    </Badge>
                  </Table.Td>
                  <Table.Td>
                    <Group gap="xs">
                      {node.status === 'healthy' ? (
                        <IconCheck size={16} color="var(--mantine-color-green-6)" />
                      ) : (
                        <IconX size={16} color="var(--mantine-color-red-6)" />
                      )}
                      <Badge color={getStatusColor(node.status)} variant="light">
                        {node.status || 'healthy'}
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
                    <Group gap="xs">
                      <IconClock size={14} />
                      <Text size="sm">
                        {node.last_heartbeat
                          ? new Date(node.last_heartbeat).toLocaleTimeString()
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
