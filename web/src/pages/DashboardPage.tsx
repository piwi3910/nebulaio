import { useQuery } from '@tanstack/react-query';
import {
  Grid,
  Card,
  Text,
  Title,
  Group,
  RingProgress,
  SimpleGrid,
  Paper,
  ThemeIcon,
  Skeleton,
  Badge,
} from '@mantine/core';
import {
  IconBucket,
  IconUsers,
  IconServer,
  IconDatabase,
  IconCheck,
  IconAlertTriangle,
} from '@tabler/icons-react';
import { adminApi } from '../api/client';

interface StatCardProps {
  title: string;
  value: string | number;
  icon: React.ReactNode;
  color: string;
  description?: string;
}

function StatCard({ title, value, icon, color, description }: StatCardProps) {
  return (
    <Paper withBorder p="md" radius="md">
      <Group justify="space-between">
        <div>
          <Text c="dimmed" size="xs" tt="uppercase" fw={700}>
            {title}
          </Text>
          <Text fw={700} size="xl">
            {value}
          </Text>
          {description && (
            <Text c="dimmed" size="sm" mt={4}>
              {description}
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

export function DashboardPage() {
  const { data: buckets, isLoading: bucketsLoading } = useQuery({
    queryKey: ['buckets'],
    queryFn: () => adminApi.listBuckets().then((res) => res.data),
  });

  const { data: users, isLoading: usersLoading } = useQuery({
    queryKey: ['users'],
    queryFn: () => adminApi.listUsers().then((res) => res.data),
  });

  const { data: clusterStatus, isLoading: clusterLoading } = useQuery({
    queryKey: ['cluster-status'],
    queryFn: () => adminApi.getClusterStatus().then((res) => res.data),
  });

  const { data: storageInfo, isLoading: storageLoading } = useQuery({
    queryKey: ['storage-info'],
    queryFn: () => adminApi.getStorageInfo().then((res) => res.data),
  });

  const isLoading = bucketsLoading || usersLoading || clusterLoading || storageLoading;

  // Calculate storage usage percentage (placeholder)
  const storageUsedPercent = 35;

  return (
    <div>
      <Title order={2} mb="lg">
        Dashboard
      </Title>

      {/* Stats Grid */}
      <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }} mb="lg">
        {isLoading ? (
          <>
            <Skeleton height={100} />
            <Skeleton height={100} />
            <Skeleton height={100} />
            <Skeleton height={100} />
          </>
        ) : (
          <>
            <StatCard
              title="Buckets"
              value={buckets?.length || 0}
              icon={<IconBucket size={24} />}
              color="blue"
            />
            <StatCard
              title="Users"
              value={users?.length || 0}
              icon={<IconUsers size={24} />}
              color="green"
            />
            <StatCard
              title="Nodes"
              value={clusterStatus?.nodes?.length || 1}
              icon={<IconServer size={24} />}
              color="violet"
            />
            <StatCard
              title="Storage Used"
              value={`${storageUsedPercent}%`}
              icon={<IconDatabase size={24} />}
              color="orange"
            />
          </>
        )}
      </SimpleGrid>

      <Grid>
        {/* Cluster Status */}
        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder radius="md" p="lg">
            <Title order={4} mb="md">
              Cluster Status
            </Title>
            {clusterLoading ? (
              <Skeleton height={150} />
            ) : (
              <div>
                <Group mb="sm">
                  <Text fw={500}>State:</Text>
                  <Badge
                    color={clusterStatus?.raft_state === 'Leader' ? 'green' : 'blue'}
                    variant="light"
                  >
                    {clusterStatus?.raft_state || 'Unknown'}
                  </Badge>
                </Group>
                <Group mb="sm">
                  <Text fw={500}>Leader:</Text>
                  <Text c="dimmed">{clusterStatus?.leader_address || 'N/A'}</Text>
                </Group>
                <Group mb="sm">
                  <Text fw={500}>Cluster ID:</Text>
                  <Text c="dimmed" size="sm">
                    {clusterStatus?.cluster_id || 'N/A'}
                  </Text>
                </Group>
              </div>
            )}
          </Card>
        </Grid.Col>

        {/* Storage Overview */}
        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder radius="md" p="lg">
            <Title order={4} mb="md">
              Storage Overview
            </Title>
            {storageLoading ? (
              <Skeleton height={150} />
            ) : (
              <Group justify="center">
                <RingProgress
                  size={150}
                  thickness={16}
                  roundCaps
                  sections={[{ value: storageUsedPercent, color: 'blue' }]}
                  label={
                    <Text ta="center" fw={700} size="lg">
                      {storageUsedPercent}%
                    </Text>
                  }
                />
                <div>
                  <Text size="sm" c="dimmed">
                    Used Space
                  </Text>
                  <Text fw={500}>3.5 GB</Text>
                  <Text size="sm" c="dimmed" mt="xs">
                    Available
                  </Text>
                  <Text fw={500}>10 GB</Text>
                </div>
              </Group>
            )}
          </Card>
        </Grid.Col>

        {/* Recent Buckets */}
        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder radius="md" p="lg">
            <Title order={4} mb="md">
              Recent Buckets
            </Title>
            {bucketsLoading ? (
              <Skeleton height={150} />
            ) : buckets?.length ? (
              buckets.slice(0, 5).map((bucket: { name: string; created_at: string }) => (
                <Group key={bucket.name} justify="space-between" mb="xs">
                  <Group gap="xs">
                    <IconBucket size={16} />
                    <Text size="sm">{bucket.name}</Text>
                  </Group>
                  <Text size="xs" c="dimmed">
                    {new Date(bucket.created_at).toLocaleDateString()}
                  </Text>
                </Group>
              ))
            ) : (
              <Text c="dimmed" ta="center">
                No buckets created yet
              </Text>
            )}
          </Card>
        </Grid.Col>

        {/* System Health */}
        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder radius="md" p="lg">
            <Title order={4} mb="md">
              System Health
            </Title>
            <div>
              <Group mb="xs">
                <ThemeIcon color="green" size="sm" radius="xl">
                  <IconCheck size={12} />
                </ThemeIcon>
                <Text size="sm">S3 API Server</Text>
                <Badge color="green" size="xs" variant="light">
                  Healthy
                </Badge>
              </Group>
              <Group mb="xs">
                <ThemeIcon color="green" size="sm" radius="xl">
                  <IconCheck size={12} />
                </ThemeIcon>
                <Text size="sm">Admin API Server</Text>
                <Badge color="green" size="xs" variant="light">
                  Healthy
                </Badge>
              </Group>
              <Group mb="xs">
                <ThemeIcon color="green" size="sm" radius="xl">
                  <IconCheck size={12} />
                </ThemeIcon>
                <Text size="sm">Metadata Store (Raft)</Text>
                <Badge color="green" size="xs" variant="light">
                  Healthy
                </Badge>
              </Group>
              <Group mb="xs">
                <ThemeIcon
                  color={storageInfo?.is_leader ? 'green' : 'yellow'}
                  size="sm"
                  radius="xl"
                >
                  {storageInfo?.is_leader ? (
                    <IconCheck size={12} />
                  ) : (
                    <IconAlertTriangle size={12} />
                  )}
                </ThemeIcon>
                <Text size="sm">Raft Leadership</Text>
                <Badge
                  color={storageInfo?.is_leader ? 'green' : 'yellow'}
                  size="xs"
                  variant="light"
                >
                  {storageInfo?.is_leader ? 'Leader' : 'Follower'}
                </Badge>
              </Group>
            </div>
          </Card>
        </Grid.Col>
      </Grid>
    </div>
  );
}
