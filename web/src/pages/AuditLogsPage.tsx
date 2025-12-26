import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  Title,
  Group,
  Paper,
  Table,
  Text,
  Badge,
  Skeleton,
  Stack,
  Select,
  TextInput,
  Button,
  Modal,
  Code,
  Pagination,
  ActionIcon,
  Tooltip,
  Card,
  Grid,
} from '@mantine/core';
// Using native date input since @mantine/dates is not installed
import {
  IconFilter,
  IconRefresh,
  IconEye,
  IconUser,
  IconBucket,
  IconCalendar,
  IconShieldCheck,
  IconX,
} from '@tabler/icons-react';
import { adminApi, AuditLogEntry } from '../api/client';
import dayjs from 'dayjs';

interface AuditLogsResponse {
  logs: AuditLogEntry[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
}

const EVENT_TYPE_COLORS: Record<string, string> = {
  'object:upload': 'blue',
  'object:download': 'green',
  'object:delete': 'red',
  'bucket:create': 'violet',
  'bucket:delete': 'red',
  'user:login': 'cyan',
  'user:logout': 'gray',
  'user:create': 'violet',
  'user:delete': 'red',
  'policy:create': 'violet',
  'policy:update': 'yellow',
  'policy:delete': 'red',
  'key:create': 'violet',
  'key:delete': 'red',
};

function getEventTypeColor(eventType: string): string {
  return EVENT_TYPE_COLORS[eventType] || 'gray';
}

function formatEventType(eventType: string): string {
  return eventType.replace(':', ' ').replace(/_/g, ' ');
}

export function AuditLogsPage() {
  const [page, setPage] = useState(1);
  const [pageSize] = useState(25);
  const [filters, setFilters] = useState<{
    bucket?: string;
    user_id?: string;
    event_type?: string;
    start_date?: Date | null;
    end_date?: Date | null;
  }>({});
  const [selectedLog, setSelectedLog] = useState<AuditLogEntry | null>(null);
  const [showFilters, setShowFilters] = useState(false);

  // Static event types for filter dropdown
  const eventTypes = [
    'object:upload',
    'object:download',
    'object:delete',
    'bucket:create',
    'bucket:delete',
    'user:login',
    'user:logout',
    'user:create',
    'user:delete',
    'policy:create',
    'policy:update',
    'policy:delete',
    'key:create',
    'key:delete',
  ];

  // Fetch audit logs
  const { data, isLoading, refetch } = useQuery({
    queryKey: ['audit-logs', page, pageSize, filters],
    queryFn: () =>
      adminApi
        .listAuditLogs({
          page,
          page_size: pageSize,
          bucket: filters.bucket || undefined,
          user_id: filters.user_id || undefined,
          event_type: filters.event_type || undefined,
          start_date: filters.start_date ? dayjs(filters.start_date).format('YYYY-MM-DD') : undefined,
          end_date: filters.end_date ? dayjs(filters.end_date).format('YYYY-MM-DD') : undefined,
        })
        .then((res) => res.data as AuditLogsResponse),
    refetchInterval: 30000, // Refresh every 30 seconds
  });

  const logs = data?.logs || [];
  const totalPages = data?.total_pages || 1;
  const totalLogs = data?.total || 0;

  const clearFilters = () => {
    setFilters({});
    setPage(1);
  };

  const hasActiveFilters =
    filters.bucket || filters.user_id || filters.event_type || filters.start_date || filters.end_date;

  // Stats for quick overview
  const recentStats = {
    total: totalLogs,
    errors: logs.filter((l) => l.status_code >= 400).length,
    writes: logs.filter((l) => ['object:upload', 'object:delete', 'bucket:create', 'bucket:delete'].includes(l.event_type)).length,
    reads: logs.filter((l) => ['object:download'].includes(l.event_type)).length,
  };

  return (
    <div>
      <Group justify="space-between" mb="lg">
        <Group>
          <IconShieldCheck size={28} color="var(--mantine-color-blue-6)" />
          <Title order={2}>Audit Logs</Title>
        </Group>
        <Group>
          <Tooltip label="Refresh">
            <ActionIcon variant="subtle" size="lg" onClick={() => refetch()}>
              <IconRefresh size={20} />
            </ActionIcon>
          </Tooltip>
          <Button
            variant={showFilters ? 'filled' : 'light'}
            leftSection={<IconFilter size={16} />}
            onClick={() => setShowFilters(!showFilters)}
          >
            Filters
          </Button>
        </Group>
      </Group>

      {/* Stats Cards */}
      <Grid mb="lg">
        <Grid.Col span={{ base: 6, md: 3 }}>
          <Card withBorder p="md">
            <Text size="xs" c="dimmed" tt="uppercase" fw={700}>
              Total Events
            </Text>
            <Text size="xl" fw={700}>
              {totalLogs.toLocaleString()}
            </Text>
          </Card>
        </Grid.Col>
        <Grid.Col span={{ base: 6, md: 3 }}>
          <Card withBorder p="md">
            <Text size="xs" c="dimmed" tt="uppercase" fw={700}>
              Read Operations
            </Text>
            <Text size="xl" fw={700} c="green">
              {recentStats.reads}
            </Text>
          </Card>
        </Grid.Col>
        <Grid.Col span={{ base: 6, md: 3 }}>
          <Card withBorder p="md">
            <Text size="xs" c="dimmed" tt="uppercase" fw={700}>
              Write Operations
            </Text>
            <Text size="xl" fw={700} c="blue">
              {recentStats.writes}
            </Text>
          </Card>
        </Grid.Col>
        <Grid.Col span={{ base: 6, md: 3 }}>
          <Card withBorder p="md">
            <Text size="xs" c="dimmed" tt="uppercase" fw={700}>
              Errors
            </Text>
            <Text size="xl" fw={700} c={recentStats.errors > 0 ? 'red' : 'gray'}>
              {recentStats.errors}
            </Text>
          </Card>
        </Grid.Col>
      </Grid>

      {/* Filters Panel */}
      {showFilters && (
        <Paper withBorder p="md" mb="lg">
          <Group justify="space-between" mb="md">
            <Text fw={500}>Filter Logs</Text>
            {hasActiveFilters && (
              <Button variant="subtle" size="xs" leftSection={<IconX size={14} />} onClick={clearFilters}>
                Clear filters
              </Button>
            )}
          </Group>
          <Grid>
            <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
              <Select
                label="Event Type"
                placeholder="All events"
                clearable
                leftSection={<IconFilter size={16} />}
                value={filters.event_type || null}
                onChange={(value) => {
                  setFilters({ ...filters, event_type: value || undefined });
                  setPage(1);
                }}
                data={eventTypes.map((type) => ({
                  value: type,
                  label: formatEventType(type),
                }))}
              />
            </Grid.Col>
            <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
              <TextInput
                label="Bucket"
                placeholder="Filter by bucket"
                leftSection={<IconBucket size={16} />}
                value={filters.bucket || ''}
                onChange={(e) => {
                  setFilters({ ...filters, bucket: e.target.value || undefined });
                  setPage(1);
                }}
              />
            </Grid.Col>
            <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
              <TextInput
                label="User ID"
                placeholder="Filter by user"
                leftSection={<IconUser size={16} />}
                value={filters.user_id || ''}
                onChange={(e) => {
                  setFilters({ ...filters, user_id: e.target.value || undefined });
                  setPage(1);
                }}
              />
            </Grid.Col>
            <Grid.Col span={{ base: 6, sm: 3, md: 1.5 }}>
              <TextInput
                label="Start Date"
                type="date"
                leftSection={<IconCalendar size={16} />}
                value={filters.start_date ? dayjs(filters.start_date).format('YYYY-MM-DD') : ''}
                onChange={(e) => {
                  setFilters({
                    ...filters,
                    start_date: e.target.value ? new Date(e.target.value) : undefined,
                  });
                  setPage(1);
                }}
              />
            </Grid.Col>
            <Grid.Col span={{ base: 6, sm: 3, md: 1.5 }}>
              <TextInput
                label="End Date"
                type="date"
                leftSection={<IconCalendar size={16} />}
                value={filters.end_date ? dayjs(filters.end_date).format('YYYY-MM-DD') : ''}
                onChange={(e) => {
                  setFilters({
                    ...filters,
                    end_date: e.target.value ? new Date(e.target.value) : undefined,
                  });
                  setPage(1);
                }}
              />
            </Grid.Col>
          </Grid>
        </Paper>
      )}

      {/* Logs Table */}
      <Paper withBorder>
        {isLoading ? (
          <Stack p="md">
            <Skeleton height={40} />
            <Skeleton height={40} />
            <Skeleton height={40} />
            <Skeleton height={40} />
            <Skeleton height={40} />
          </Stack>
        ) : logs.length ? (
          <>
            <Table striped highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Timestamp</Table.Th>
                  <Table.Th>Event</Table.Th>
                  <Table.Th>User</Table.Th>
                  <Table.Th>Bucket</Table.Th>
                  <Table.Th>Object</Table.Th>
                  <Table.Th>Status</Table.Th>
                  <Table.Th>Source IP</Table.Th>
                  <Table.Th style={{ width: 60 }}>Details</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {logs.map((log) => (
                  <Table.Tr key={log.id}>
                    <Table.Td>
                      <Text size="sm">{dayjs(log.timestamp).format('YYYY-MM-DD HH:mm:ss')}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Badge color={getEventTypeColor(log.event_type)} variant="light" size="sm">
                        {formatEventType(log.event_type)}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <Group gap={4}>
                        <IconUser size={14} />
                        <Text size="sm">{log.username || log.user_id?.slice(0, 8) || 'anonymous'}</Text>
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm">{log.bucket || '-'}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Tooltip label={log.object_key || '-'} disabled={!log.object_key}>
                        <Text size="sm" lineClamp={1} style={{ maxWidth: 150 }}>
                          {log.object_key || '-'}
                        </Text>
                      </Tooltip>
                    </Table.Td>
                    <Table.Td>
                      <Badge
                        color={log.status_code < 400 ? 'green' : 'red'}
                        variant="light"
                        size="sm"
                      >
                        {log.status_code}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <Code fz="xs">{log.source_ip}</Code>
                    </Table.Td>
                    <Table.Td>
                      <Tooltip label="View details">
                        <ActionIcon variant="subtle" onClick={() => setSelectedLog(log)}>
                          <IconEye size={16} />
                        </ActionIcon>
                      </Tooltip>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>

            {/* Pagination */}
            <Group justify="space-between" p="md">
              <Text size="sm" c="dimmed">
                Showing {(page - 1) * pageSize + 1} - {Math.min(page * pageSize, totalLogs)} of{' '}
                {totalLogs} logs
              </Text>
              <Pagination total={totalPages} value={page} onChange={setPage} size="sm" />
            </Group>
          </>
        ) : (
          <Text c="dimmed" ta="center" p="xl">
            {hasActiveFilters
              ? 'No logs found matching your filters'
              : 'No audit logs available'}
          </Text>
        )}
      </Paper>

      {/* Log Details Modal */}
      <Modal
        opened={selectedLog !== null}
        onClose={() => setSelectedLog(null)}
        title="Audit Log Details"
        size="lg"
      >
        {selectedLog && (
          <Stack>
            <Group>
              <Text size="sm" c="dimmed" w={120}>
                Event ID:
              </Text>
              <Code>{selectedLog.id}</Code>
            </Group>
            <Group>
              <Text size="sm" c="dimmed" w={120}>
                Timestamp:
              </Text>
              <Text size="sm">
                {dayjs(selectedLog.timestamp).format('YYYY-MM-DD HH:mm:ss.SSS')}
              </Text>
            </Group>
            <Group>
              <Text size="sm" c="dimmed" w={120}>
                Event Type:
              </Text>
              <Badge color={getEventTypeColor(selectedLog.event_type)} variant="light">
                {formatEventType(selectedLog.event_type)}
              </Badge>
            </Group>
            <Group>
              <Text size="sm" c="dimmed" w={120}>
                User:
              </Text>
              <div>
                <Text size="sm" fw={500}>
                  {selectedLog.username || 'anonymous'}
                </Text>
                {selectedLog.user_id && (
                  <Code fz="xs">{selectedLog.user_id}</Code>
                )}
              </div>
            </Group>
            <Group>
              <Text size="sm" c="dimmed" w={120}>
                Bucket:
              </Text>
              <Text size="sm">{selectedLog.bucket || '-'}</Text>
            </Group>
            <Group>
              <Text size="sm" c="dimmed" w={120}>
                Object Key:
              </Text>
              <Code style={{ wordBreak: 'break-all' }}>{selectedLog.object_key || '-'}</Code>
            </Group>
            <Group>
              <Text size="sm" c="dimmed" w={120}>
                Status Code:
              </Text>
              <Badge color={selectedLog.status_code < 400 ? 'green' : 'red'} variant="light">
                {selectedLog.status_code}
              </Badge>
            </Group>
            <Group>
              <Text size="sm" c="dimmed" w={120}>
                Source IP:
              </Text>
              <Code>{selectedLog.source_ip}</Code>
            </Group>
            <Group>
              <Text size="sm" c="dimmed" w={120}>
                User Agent:
              </Text>
              <Text size="xs" style={{ wordBreak: 'break-all' }}>
                {selectedLog.user_agent}
              </Text>
            </Group>
            <Group>
              <Text size="sm" c="dimmed" w={120}>
                Request ID:
              </Text>
              <Code>{selectedLog.request_id}</Code>
            </Group>
            {selectedLog.details && Object.keys(selectedLog.details).length > 0 && (
              <div>
                <Text size="sm" c="dimmed" mb="xs">
                  Additional Details:
                </Text>
                <Code block style={{ whiteSpace: 'pre-wrap' }}>
                  {JSON.stringify(selectedLog.details, null, 2)}
                </Code>
              </div>
            )}
          </Stack>
        )}
      </Modal>
    </div>
  );
}
