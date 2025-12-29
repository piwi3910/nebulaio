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
  Skeleton,
  TextInput,
  NumberInput,
  Button,
  Code,
  ThemeIcon,
  Alert,
  Tooltip,
  ActionIcon,
  Tabs,
  SimpleGrid,
  Table,
  Modal,
  Textarea,
  Select,
  RingProgress,
  Center,
} from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { notifications } from '@mantine/notifications';
import {
  IconShield,
  IconKey,
  IconLock,
  IconRoute,
  IconAlertTriangle,
  IconInfoCircle,
  IconRefresh,
  IconSettings,
  IconChartBar,
  IconCertificate,
  IconBan,
  IconRotate,
  IconActivity,
  IconUsers,
  IconFlame,
  IconExternalLink,
} from '@tabler/icons-react';
import {
  adminApi,
  Anomaly,
  AnalyticsStats,
  EncryptionKey,
  Certificate,
  TracingStats,
} from '../api/client';

// Documentation base URL - can be overridden via environment variable
const DOCS_BASE_URL = import.meta.env.VITE_DOCS_URL || 'https://github.com/piwi3910/nebulaio/blob/main/docs';

interface SecurityConfig {
  access_analytics?: {
    enabled?: boolean;
    baseline_window?: string;
    anomaly_threshold?: number;
    sampling_rate?: number;
  };
  key_rotation?: {
    enabled?: boolean;
    check_interval?: string;
  };
  mtls?: {
    enabled?: boolean;
    require_client_cert?: boolean;
  };
  tracing?: {
    enabled?: boolean;
    sampling_rate?: number;
    exporter_type?: string;
    endpoint?: string;
  };
}

function getSeverityColor(severity: string): string {
  switch (severity) {
    case 'CRITICAL':
      return 'red';
    case 'HIGH':
      return 'orange';
    case 'MEDIUM':
      return 'yellow';
    case 'LOW':
      return 'blue';
    default:
      return 'gray';
  }
}

function formatDate(dateStr: string): string {
  return new Date(dateStr).toLocaleString();
}

// Access Analytics Tab
function AccessAnalyticsTab() {
  const [selectedAnomaly, setSelectedAnomaly] = useState<Anomaly | null>(null);
  const [acknowledgeOpened, { open: openAcknowledge, close: closeAcknowledge }] = useDisclosure(false);
  const [resolution, setResolution] = useState('');
  const queryClient = useQueryClient();

  const { data: stats, isLoading: statsLoading, refetch: refetchStats } = useQuery({
    queryKey: ['analytics-stats'],
    queryFn: async () => {
      try {
        const res = await adminApi.getAnalyticsStats();
        return res.data as AnalyticsStats;
      } catch {
        return null;
      }
    },
    refetchInterval: 30000,
  });

  const { data: anomalies, isLoading: anomaliesLoading } = useQuery({
    queryKey: ['anomalies'],
    queryFn: async () => {
      try {
        const res = await adminApi.listAnomalies({ limit: 50 });
        return res.data.anomalies as Anomaly[];
      } catch {
        return [];
      }
    },
    refetchInterval: 30000,
  });

  const acknowledgeMutation = useMutation({
    mutationFn: async ({ id, resolution }: { id: string; resolution: string }) => {
      return adminApi.acknowledgeAnomaly(id, { resolution });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['anomalies'] });
      closeAcknowledge();
      notifications.show({
        title: 'Anomaly Acknowledged',
        message: 'The anomaly has been marked as acknowledged',
        color: 'green',
      });
    },
  });

  const handleAcknowledge = (anomaly: Anomaly) => {
    setSelectedAnomaly(anomaly);
    setResolution('');
    openAcknowledge();
  };

  if (statsLoading) {
    return <Skeleton height={400} />;
  }

  return (
    <Stack>
      {/* Overview Cards */}
      <SimpleGrid cols={{ base: 2, sm: 4 }}>
        <Card withBorder p="md">
          <Group>
            <ThemeIcon size="lg" color="blue" variant="light">
              <IconActivity size={20} />
            </ThemeIcon>
            <div>
              <Text size="xs" c="dimmed">Total Events</Text>
              <Text fw={700} size="xl">{stats?.total_events?.toLocaleString() ?? 0}</Text>
            </div>
          </Group>
        </Card>
        <Card withBorder p="md">
          <Group>
            <ThemeIcon size="lg" color="green" variant="light">
              <IconUsers size={20} />
            </ThemeIcon>
            <div>
              <Text size="xs" c="dimmed">Users Monitored</Text>
              <Text fw={700} size="xl">{stats?.users_monitored ?? 0}</Text>
            </div>
          </Group>
        </Card>
        <Card withBorder p="md">
          <Group>
            <ThemeIcon size="lg" color="orange" variant="light">
              <IconAlertTriangle size={20} />
            </ThemeIcon>
            <div>
              <Text size="xs" c="dimmed">Anomalies</Text>
              <Text fw={700} size="xl">{stats?.anomalies_detected ?? 0}</Text>
            </div>
          </Group>
        </Card>
        <Card withBorder p="md">
          <Group>
            <ThemeIcon size="lg" color="violet" variant="light">
              <IconChartBar size={20} />
            </ThemeIcon>
            <div>
              <Text size="xs" c="dimmed">Events (24h)</Text>
              <Text fw={700} size="xl">{stats?.events_last_24h?.toLocaleString() ?? 0}</Text>
            </div>
          </Group>
        </Card>
      </SimpleGrid>

      {/* Anomalies by Severity */}
      <Grid>
        <Grid.Col span={{ base: 12, md: 4 }}>
          <Card withBorder p="md">
            <Text fw={500} mb="md">Anomalies by Severity</Text>
            <Stack gap="xs">
              {['CRITICAL', 'HIGH', 'MEDIUM', 'LOW'].map((severity) => (
                <Group key={severity} justify="space-between">
                  <Badge color={getSeverityColor(severity)} variant="light">
                    {severity}
                  </Badge>
                  <Text fw={500}>{stats?.anomalies_by_severity?.[severity] ?? 0}</Text>
                </Group>
              ))}
            </Stack>
          </Card>
        </Grid.Col>

        <Grid.Col span={{ base: 12, md: 8 }}>
          <Card withBorder p="md">
            <Group justify="space-between" mb="md">
              <Text fw={500}>Recent Anomalies</Text>
              <Button
                variant="subtle"
                size="xs"
                leftSection={<IconRefresh size={14} />}
                onClick={() => refetchStats()}
              >
                Refresh
              </Button>
            </Group>
            {anomaliesLoading ? (
              <Skeleton height={200} />
            ) : anomalies && anomalies.length > 0 ? (
              <Table>
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>Type</Table.Th>
                    <Table.Th>Severity</Table.Th>
                    <Table.Th>User</Table.Th>
                    <Table.Th>Time</Table.Th>
                    <Table.Th>Actions</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {anomalies.slice(0, 10).map((anomaly) => (
                    <Table.Tr key={anomaly.id}>
                      <Table.Td>
                        <Text size="sm">{anomaly.type.replace(/_/g, ' ')}</Text>
                      </Table.Td>
                      <Table.Td>
                        <Badge color={getSeverityColor(anomaly.severity)} size="sm">
                          {anomaly.severity}
                        </Badge>
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm">{anomaly.user_id}</Text>
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm">{formatDate(anomaly.detected_at)}</Text>
                      </Table.Td>
                      <Table.Td>
                        {anomaly.acknowledged ? (
                          <Badge color="green" variant="light" size="sm">Acknowledged</Badge>
                        ) : (
                          <Button
                            size="xs"
                            variant="light"
                            onClick={() => handleAcknowledge(anomaly)}
                          >
                            Acknowledge
                          </Button>
                        )}
                      </Table.Td>
                    </Table.Tr>
                  ))}
                </Table.Tbody>
              </Table>
            ) : (
              <Text c="dimmed" ta="center" py="xl">No anomalies detected</Text>
            )}
          </Card>
        </Grid.Col>
      </Grid>

      {/* Acknowledge Modal */}
      <Modal opened={acknowledgeOpened} onClose={closeAcknowledge} title="Acknowledge Anomaly">
        <Stack>
          {selectedAnomaly && (
            <>
              <Alert color={getSeverityColor(selectedAnomaly.severity)} icon={<IconAlertTriangle />}>
                <Text size="sm" fw={500}>{selectedAnomaly.type.replace(/_/g, ' ')}</Text>
                <Text size="xs">{selectedAnomaly.description}</Text>
              </Alert>
              <Textarea
                label="Resolution"
                placeholder="Describe how this anomaly was resolved or why it's not a concern..."
                value={resolution}
                onChange={(e) => setResolution(e.target.value)}
                minRows={3}
              />
              <Group justify="flex-end">
                <Button variant="default" onClick={closeAcknowledge}>Cancel</Button>
                <Button
                  onClick={() => acknowledgeMutation.mutate({ id: selectedAnomaly.id, resolution })}
                  loading={acknowledgeMutation.isPending}
                >
                  Acknowledge
                </Button>
              </Group>
            </>
          )}
        </Stack>
      </Modal>
    </Stack>
  );
}

// Key Rotation Tab
function KeyRotationTab() {
  const [rotateOpened, { open: openRotate, close: closeRotate }] = useDisclosure(false);
  const [selectedKey, setSelectedKey] = useState<EncryptionKey | null>(null);
  const queryClient = useQueryClient();

  const { data: keys, isLoading: keysLoading, refetch: refetchKeys } = useQuery({
    queryKey: ['encryption-keys'],
    queryFn: async () => {
      try {
        const res = await adminApi.listKeys();
        return res.data.keys as EncryptionKey[];
      } catch {
        return [];
      }
    },
  });

  const rotateMutation = useMutation({
    mutationFn: async (keyId: string) => {
      return adminApi.rotateKey(keyId, { trigger: 'manual' });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['encryption-keys'] });
      closeRotate();
      notifications.show({
        title: 'Key Rotated',
        message: 'The encryption key has been rotated successfully',
        color: 'green',
      });
    },
    onError: () => {
      notifications.show({
        title: 'Rotation Failed',
        message: 'Failed to rotate the encryption key',
        color: 'red',
      });
    },
  });

  const handleRotate = (key: EncryptionKey) => {
    setSelectedKey(key);
    openRotate();
  };

  const getKeyTypeColor = (type: string): string => {
    switch (type) {
      case 'MASTER':
        return 'red';
      case 'DATA':
        return 'blue';
      case 'BUCKET':
        return 'green';
      case 'OBJECT':
        return 'violet';
      default:
        return 'gray';
    }
  };

  const getStatusColor = (status: string): string => {
    switch (status) {
      case 'ACTIVE':
        return 'green';
      case 'ROTATING':
        return 'yellow';
      case 'DISABLED':
        return 'gray';
      case 'PENDING_DELETION':
        return 'red';
      default:
        return 'gray';
    }
  };

  return (
    <Stack>
      <Group justify="space-between">
        <Text fw={500}>Encryption Keys</Text>
        <Button
          variant="light"
          leftSection={<IconRefresh size={16} />}
          onClick={() => refetchKeys()}
        >
          Refresh
        </Button>
      </Group>

      {keysLoading ? (
        <Skeleton height={300} />
      ) : keys && keys.length > 0 ? (
        <Table>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Alias</Table.Th>
              <Table.Th>Type</Table.Th>
              <Table.Th>Algorithm</Table.Th>
              <Table.Th>Version</Table.Th>
              <Table.Th>Status</Table.Th>
              <Table.Th>Last Rotated</Table.Th>
              <Table.Th>Expires</Table.Th>
              <Table.Th>Actions</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {keys.map((key) => (
              <Table.Tr key={key.id}>
                <Table.Td>
                  <Text size="sm" fw={500}>{key.alias || key.id.slice(0, 8)}</Text>
                </Table.Td>
                <Table.Td>
                  <Badge color={getKeyTypeColor(key.type)} variant="light" size="sm">
                    {key.type}
                  </Badge>
                </Table.Td>
                <Table.Td>
                  <Code>{key.algorithm}</Code>
                </Table.Td>
                <Table.Td>
                  <Text size="sm">v{key.version}</Text>
                </Table.Td>
                <Table.Td>
                  <Badge color={getStatusColor(key.status)} size="sm">
                    {key.status}
                  </Badge>
                </Table.Td>
                <Table.Td>
                  <Text size="sm">{key.rotated_at ? formatDate(key.rotated_at) : 'Never'}</Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm">{key.expires_at ? formatDate(key.expires_at) : 'N/A'}</Text>
                </Table.Td>
                <Table.Td>
                  <Group gap="xs">
                    <Tooltip label="Rotate Key">
                      <ActionIcon
                        variant="light"
                        color="blue"
                        onClick={() => handleRotate(key)}
                        disabled={key.status !== 'ACTIVE'}
                      >
                        <IconRotate size={16} />
                      </ActionIcon>
                    </Tooltip>
                  </Group>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      ) : (
        <Card withBorder p="xl">
          <Center>
            <Stack align="center" gap="sm">
              <ThemeIcon size="xl" color="gray" variant="light">
                <IconKey size={24} />
              </ThemeIcon>
              <Text c="dimmed">No encryption keys found</Text>
            </Stack>
          </Center>
        </Card>
      )}

      {/* Rotate Confirmation Modal */}
      <Modal opened={rotateOpened} onClose={closeRotate} title="Rotate Encryption Key">
        <Stack>
          {selectedKey && (
            <>
              <Alert color="yellow" icon={<IconInfoCircle />}>
                <Text size="sm">
                  This will create a new version of the key. Objects encrypted with the old version
                  will need to be re-encrypted to use the new key.
                </Text>
              </Alert>
              <Card withBorder p="sm">
                <Group justify="space-between">
                  <Text size="sm" c="dimmed">Key</Text>
                  <Text size="sm" fw={500}>{selectedKey.alias || selectedKey.id}</Text>
                </Group>
                <Group justify="space-between" mt="xs">
                  <Text size="sm" c="dimmed">Current Version</Text>
                  <Text size="sm">v{selectedKey.version}</Text>
                </Group>
                <Group justify="space-between" mt="xs">
                  <Text size="sm" c="dimmed">New Version</Text>
                  <Text size="sm" c="green">v{selectedKey.version + 1}</Text>
                </Group>
              </Card>
              <Group justify="flex-end">
                <Button variant="default" onClick={closeRotate}>Cancel</Button>
                <Button
                  color="blue"
                  onClick={() => rotateMutation.mutate(selectedKey.id)}
                  loading={rotateMutation.isPending}
                >
                  Rotate Key
                </Button>
              </Group>
            </>
          )}
        </Stack>
      </Modal>
    </Stack>
  );
}

// mTLS Tab
function MTLSTab() {
  const [revokeOpened, { open: openRevoke, close: closeRevoke }] = useDisclosure(false);
  const [selectedCert, setSelectedCert] = useState<Certificate | null>(null);
  const [revokeReason, setRevokeReason] = useState('');
  const queryClient = useQueryClient();

  const { data: certificates, isLoading: certsLoading, refetch: refetchCerts } = useQuery({
    queryKey: ['certificates'],
    queryFn: async () => {
      try {
        const res = await adminApi.listCertificates();
        return res.data.certificates as Certificate[];
      } catch {
        return [];
      }
    },
  });

  const revokeMutation = useMutation({
    mutationFn: async ({ id, reason }: { id: string; reason: string }) => {
      return adminApi.revokeCertificate(id, { reason });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['certificates'] });
      closeRevoke();
      notifications.show({
        title: 'Certificate Revoked',
        message: 'The certificate has been revoked',
        color: 'green',
      });
    },
  });

  const handleRevoke = (cert: Certificate) => {
    setSelectedCert(cert);
    setRevokeReason('');
    openRevoke();
  };

  const getCertTypeColor = (type: string): string => {
    switch (type) {
      case 'CA':
        return 'red';
      case 'SERVER':
        return 'blue';
      case 'CLIENT':
        return 'green';
      case 'PEER':
        return 'violet';
      default:
        return 'gray';
    }
  };

  const isExpiringSoon = (expiresAt: string): boolean => {
    const expiry = new Date(expiresAt);
    const now = new Date();
    const daysUntilExpiry = (expiry.getTime() - now.getTime()) / (1000 * 60 * 60 * 24);
    return daysUntilExpiry < 30;
  };

  return (
    <Stack>
      <Group justify="space-between">
        <Text fw={500}>Certificates</Text>
        <Button
          variant="light"
          leftSection={<IconRefresh size={16} />}
          onClick={() => refetchCerts()}
        >
          Refresh
        </Button>
      </Group>

      {certsLoading ? (
        <Skeleton height={300} />
      ) : certificates && certificates.length > 0 ? (
        <Table>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Common Name</Table.Th>
              <Table.Th>Type</Table.Th>
              <Table.Th>Serial Number</Table.Th>
              <Table.Th>Valid From</Table.Th>
              <Table.Th>Valid Until</Table.Th>
              <Table.Th>Status</Table.Th>
              <Table.Th>Actions</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {certificates.map((cert) => (
              <Table.Tr key={cert.id}>
                <Table.Td>
                  <Text size="sm" fw={500}>{cert.common_name}</Text>
                </Table.Td>
                <Table.Td>
                  <Badge color={getCertTypeColor(cert.type)} variant="light" size="sm">
                    {cert.type}
                  </Badge>
                </Table.Td>
                <Table.Td>
                  <Code>{cert.serial_number.slice(0, 16)}...</Code>
                </Table.Td>
                <Table.Td>
                  <Text size="sm">{formatDate(cert.not_before)}</Text>
                </Table.Td>
                <Table.Td>
                  <Group gap="xs">
                    <Text size="sm">{formatDate(cert.not_after)}</Text>
                    {isExpiringSoon(cert.not_after) && !cert.revoked && (
                      <Tooltip label="Expiring soon">
                        <ThemeIcon size="xs" color="yellow" variant="light">
                          <IconAlertTriangle size={12} />
                        </ThemeIcon>
                      </Tooltip>
                    )}
                  </Group>
                </Table.Td>
                <Table.Td>
                  {cert.revoked ? (
                    <Badge color="red" size="sm">Revoked</Badge>
                  ) : (
                    <Badge color="green" size="sm">Active</Badge>
                  )}
                </Table.Td>
                <Table.Td>
                  <Group gap="xs">
                    <Tooltip label="Revoke Certificate">
                      <ActionIcon
                        variant="light"
                        color="red"
                        onClick={() => handleRevoke(cert)}
                        disabled={cert.revoked}
                      >
                        <IconBan size={16} />
                      </ActionIcon>
                    </Tooltip>
                  </Group>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      ) : (
        <Card withBorder p="xl">
          <Center>
            <Stack align="center" gap="sm">
              <ThemeIcon size="xl" color="gray" variant="light">
                <IconCertificate size={24} />
              </ThemeIcon>
              <Text c="dimmed">No certificates found</Text>
            </Stack>
          </Center>
        </Card>
      )}

      {/* Revoke Modal */}
      <Modal opened={revokeOpened} onClose={closeRevoke} title="Revoke Certificate">
        <Stack>
          {selectedCert && (
            <>
              <Alert color="red" icon={<IconAlertTriangle />}>
                This action cannot be undone. The certificate will be added to the CRL.
              </Alert>
              <Card withBorder p="sm">
                <Text size="sm" c="dimmed">Certificate</Text>
                <Text size="sm" fw={500}>{selectedCert.common_name}</Text>
              </Card>
              <Select
                label="Revocation Reason"
                placeholder="Select reason"
                value={revokeReason}
                onChange={(v) => setRevokeReason(v || '')}
                data={[
                  { value: 'key_compromise', label: 'Key Compromise' },
                  { value: 'ca_compromise', label: 'CA Compromise' },
                  { value: 'affiliation_changed', label: 'Affiliation Changed' },
                  { value: 'superseded', label: 'Superseded' },
                  { value: 'cessation_of_operation', label: 'Cessation of Operation' },
                  { value: 'unspecified', label: 'Unspecified' },
                ]}
              />
              <Group justify="flex-end">
                <Button variant="default" onClick={closeRevoke}>Cancel</Button>
                <Button
                  color="red"
                  onClick={() => revokeMutation.mutate({ id: selectedCert.id, reason: revokeReason })}
                  loading={revokeMutation.isPending}
                  disabled={!revokeReason}
                >
                  Revoke Certificate
                </Button>
              </Group>
            </>
          )}
        </Stack>
      </Modal>
    </Stack>
  );
}

// Tracing Tab
function TracingTab() {
  const { data: stats, isLoading, refetch } = useQuery({
    queryKey: ['tracing-stats'],
    queryFn: async () => {
      try {
        const res = await adminApi.getTracingStats();
        return res.data as TracingStats;
      } catch {
        return null;
      }
    },
    refetchInterval: 10000,
  });

  return (
    <Stack>
      <Group justify="space-between">
        <Text fw={500}>Distributed Tracing</Text>
        <Button
          variant="light"
          leftSection={<IconRefresh size={16} />}
          onClick={() => refetch()}
          loading={isLoading}
        >
          Refresh
        </Button>
      </Group>

      {isLoading ? (
        <Skeleton height={200} />
      ) : stats ? (
        <Grid>
          <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
            <Card withBorder p="md">
              <Group>
                <RingProgress
                  size={60}
                  thickness={6}
                  sections={[{ value: (stats.sampling_rate || 0) * 100, color: 'blue' }]}
                  label={
                    <Text size="xs" ta="center" fw={700}>
                      {((stats.sampling_rate || 0) * 100).toFixed(0)}%
                    </Text>
                  }
                />
                <div>
                  <Text size="xs" c="dimmed">Sampling Rate</Text>
                  <Text fw={500}>{((stats.sampling_rate || 0) * 100).toFixed(1)}%</Text>
                </div>
              </Group>
            </Card>
          </Grid.Col>
          <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
            <Card withBorder p="md">
              <Text size="xs" c="dimmed">Active Spans</Text>
              <Text fw={700} size="xl">{stats.active_spans}</Text>
            </Card>
          </Grid.Col>
          <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
            <Card withBorder p="md">
              <Text size="xs" c="dimmed">Total Spans</Text>
              <Text fw={700} size="xl">{stats.spans_exported?.toLocaleString()}</Text>
            </Card>
          </Grid.Col>
          <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
            <Card withBorder p="md">
              <Text size="xs" c="dimmed">Exporter</Text>
              <Badge color="blue" variant="light" size="lg" mt="xs">
                {stats.exporter_type?.toUpperCase() || 'N/A'}
              </Badge>
            </Card>
          </Grid.Col>
        </Grid>
      ) : (
        <Alert color="yellow" icon={<IconInfoCircle />}>
          Tracing is not enabled or stats are unavailable
        </Alert>
      )}

      <Card withBorder p="md">
        <Text fw={500} mb="md">Trace Propagation</Text>
        <Text size="sm" c="dimmed" mb="sm">
          NebulaIO supports multiple trace context propagation formats:
        </Text>
        <SimpleGrid cols={{ base: 1, sm: 3 }}>
          <Card withBorder p="sm">
            <Group>
              <ThemeIcon color="blue" variant="light">
                <IconRoute size={16} />
              </ThemeIcon>
              <div>
                <Text size="sm" fw={500}>W3C Trace Context</Text>
                <Text size="xs" c="dimmed">traceparent, tracestate</Text>
              </div>
            </Group>
          </Card>
          <Card withBorder p="sm">
            <Group>
              <ThemeIcon color="orange" variant="light">
                <IconRoute size={16} />
              </ThemeIcon>
              <div>
                <Text size="sm" fw={500}>B3 (Zipkin)</Text>
                <Text size="xs" c="dimmed">X-B3-TraceId, X-B3-SpanId</Text>
              </div>
            </Group>
          </Card>
          <Card withBorder p="sm">
            <Group>
              <ThemeIcon color="green" variant="light">
                <IconRoute size={16} />
              </ThemeIcon>
              <div>
                <Text size="sm" fw={500}>Jaeger</Text>
                <Text size="xs" c="dimmed">uber-trace-id</Text>
              </div>
            </Group>
          </Card>
        </SimpleGrid>
      </Card>
    </Stack>
  );
}

// Rate Limiting Tab
function RateLimitingTab() {
  return (
    <Stack>
      <Alert icon={<IconInfoCircle />} color="blue">
        Rate limiting protects your storage infrastructure from abuse and DoS attacks.
        Configuration is managed via server configuration files or environment variables.
      </Alert>

      <Grid>
        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder p="md">
            <Group mb="md">
              <ThemeIcon color="orange" variant="light" size="lg">
                <IconFlame size={20} />
              </ThemeIcon>
              <div>
                <Text fw={500}>Request Rate Limiting</Text>
                <Text size="xs" c="dimmed">Control API request rates</Text>
              </div>
            </Group>
            <Stack gap="xs">
              <Group justify="space-between">
                <Text size="sm" c="dimmed">Global RPS Limit</Text>
                <Code>NEBULAIO_FIREWALL_RATE_LIMIT_RPS</Code>
              </Group>
              <Group justify="space-between">
                <Text size="sm" c="dimmed">Burst Size</Text>
                <Code>NEBULAIO_FIREWALL_RATE_LIMIT_BURST</Code>
              </Group>
              <Group justify="space-between">
                <Text size="sm" c="dimmed">Per-User Limiting</Text>
                <Code>NEBULAIO_FIREWALL_RATE_LIMIT_PER_USER</Code>
              </Group>
              <Group justify="space-between">
                <Text size="sm" c="dimmed">Per-IP Limiting</Text>
                <Code>NEBULAIO_FIREWALL_RATE_LIMIT_PER_IP</Code>
              </Group>
            </Stack>
          </Card>
        </Grid.Col>

        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder p="md">
            <Group mb="md">
              <ThemeIcon color="red" variant="light" size="lg">
                <IconShield size={20} />
              </ThemeIcon>
              <div>
                <Text fw={500}>Hash DoS Protection</Text>
                <Text size="xs" c="dimmed">Object creation rate limiting</Text>
              </div>
            </Group>
            <Stack gap="xs">
              <Group justify="space-between">
                <Text size="sm" c="dimmed">Object Creation Limit</Text>
                <Code>NEBULAIO_FIREWALL_OBJECT_CREATION_LIMIT</Code>
              </Group>
              <Group justify="space-between">
                <Text size="sm" c="dimmed">Object Creation Burst</Text>
                <Code>NEBULAIO_FIREWALL_OBJECT_CREATION_BURST</Code>
              </Group>
              <Text size="xs" c="dimmed" mt="xs">
                Protects against attacks targeting object key distribution.
                Applies to PutObject, CopyObject, CompleteMultipartUpload.
              </Text>
            </Stack>
          </Card>
        </Grid.Col>

        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder p="md">
            <Group mb="md">
              <ThemeIcon color="blue" variant="light" size="lg">
                <IconActivity size={20} />
              </ThemeIcon>
              <div>
                <Text fw={500}>Bandwidth Throttling</Text>
                <Text size="xs" c="dimmed">Control data transfer rates</Text>
              </div>
            </Group>
            <Stack gap="xs">
              <Group justify="space-between">
                <Text size="sm" c="dimmed">Max Bytes/Second</Text>
                <Code>NEBULAIO_FIREWALL_BANDWIDTH_MAX_BPS</Code>
              </Group>
              <Group justify="space-between">
                <Text size="sm" c="dimmed">Per-User Limit</Text>
                <Code>NEBULAIO_FIREWALL_BANDWIDTH_MAX_BPS_PER_USER</Code>
              </Group>
              <Group justify="space-between">
                <Text size="sm" c="dimmed">Per-Bucket Limit</Text>
                <Code>NEBULAIO_FIREWALL_BANDWIDTH_MAX_BPS_PER_BUCKET</Code>
              </Group>
            </Stack>
          </Card>
        </Grid.Col>

        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder p="md">
            <Group mb="md">
              <ThemeIcon color="green" variant="light" size="lg">
                <IconUsers size={20} />
              </ThemeIcon>
              <div>
                <Text fw={500}>Connection Limits</Text>
                <Text size="xs" c="dimmed">Control concurrent connections</Text>
              </div>
            </Group>
            <Stack gap="xs">
              <Group justify="space-between">
                <Text size="sm" c="dimmed">Max Connections</Text>
                <Code>NEBULAIO_FIREWALL_MAX_CONNECTIONS</Code>
              </Group>
              <Group justify="space-between">
                <Text size="sm" c="dimmed">Per-IP Limit</Text>
                <Code>NEBULAIO_FIREWALL_MAX_CONNECTIONS_PER_IP</Code>
              </Group>
              <Group justify="space-between">
                <Text size="sm" c="dimmed">Per-User Limit</Text>
                <Code>NEBULAIO_FIREWALL_MAX_CONNECTIONS_PER_USER</Code>
              </Group>
            </Stack>
          </Card>
        </Grid.Col>
      </Grid>

      <Card withBorder p="md">
        <Group justify="space-between">
          <div>
            <Text fw={500}>Documentation</Text>
            <Text size="sm" c="dimmed">
              For detailed configuration options, deployment examples, and best practices,
              see the rate limiting documentation.
            </Text>
          </div>
          <Button
            component="a"
            href={`${DOCS_BASE_URL}/deployment/rate-limiting.md`}
            target="_blank"
            variant="light"
            rightSection={<IconExternalLink size={16} />}
          >
            View Documentation
          </Button>
        </Group>
      </Card>
    </Stack>
  );
}

// Configuration Tab
function ConfigurationTab() {
  const queryClient = useQueryClient();

  const { data: config, isLoading } = useQuery({
    queryKey: ['security-config'],
    queryFn: async () => {
      try {
        const res = await adminApi.getSecurityConfig();
        return res.data as SecurityConfig;
      } catch {
        return null;
      }
    },
  });

  const updateMutation = useMutation({
    mutationFn: (newConfig: Partial<SecurityConfig>) =>
      adminApi.updateSecurityConfig({
        analytics_enabled: newConfig.access_analytics?.enabled,
        key_rotation_enabled: newConfig.key_rotation?.enabled,
        mtls_enabled: newConfig.mtls?.enabled,
        tracing_enabled: newConfig.tracing?.enabled,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['security-config'] });
      notifications.show({
        title: 'Configuration Updated',
        message: 'Security settings have been saved',
        color: 'green',
      });
    },
  });

  if (isLoading) {
    return <Skeleton height={400} />;
  }

  return (
    <Stack>
      <Alert icon={<IconInfoCircle />} color="blue">
        Changes to security configuration may require a server restart to take effect.
      </Alert>

      <Grid>
        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder p="md">
            <Group justify="space-between" mb="md">
              <Group>
                <ThemeIcon color="blue" variant="light">
                  <IconActivity size={20} />
                </ThemeIcon>
                <Text fw={500}>Access Analytics</Text>
              </Group>
              <Switch
                checked={config?.access_analytics?.enabled ?? false}
                onChange={(e) =>
                  updateMutation.mutate({
                    access_analytics: { ...config?.access_analytics, enabled: e.currentTarget.checked },
                  })
                }
              />
            </Group>
            <Stack gap="sm">
              <NumberInput
                label="Anomaly Threshold (std dev)"
                value={config?.access_analytics?.anomaly_threshold ?? 3.0}
                min={1}
                max={10}
                step={0.5}
                disabled={!config?.access_analytics?.enabled}
              />
              <NumberInput
                label="Sampling Rate"
                value={(config?.access_analytics?.sampling_rate ?? 1) * 100}
                min={1}
                max={100}
                suffix="%"
                disabled={!config?.access_analytics?.enabled}
              />
            </Stack>
          </Card>
        </Grid.Col>

        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder p="md">
            <Group justify="space-between" mb="md">
              <Group>
                <ThemeIcon color="green" variant="light">
                  <IconKey size={20} />
                </ThemeIcon>
                <Text fw={500}>Key Rotation</Text>
              </Group>
              <Switch
                checked={config?.key_rotation?.enabled ?? false}
                onChange={(e) =>
                  updateMutation.mutate({
                    key_rotation: { ...config?.key_rotation, enabled: e.currentTarget.checked },
                  })
                }
              />
            </Group>
            <TextInput
              label="Check Interval"
              value={config?.key_rotation?.check_interval ?? '1h'}
              disabled={!config?.key_rotation?.enabled}
            />
          </Card>
        </Grid.Col>

        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder p="md">
            <Group justify="space-between" mb="md">
              <Group>
                <ThemeIcon color="violet" variant="light">
                  <IconLock size={20} />
                </ThemeIcon>
                <Text fw={500}>mTLS</Text>
              </Group>
              <Switch
                checked={config?.mtls?.enabled ?? false}
                onChange={(e) =>
                  updateMutation.mutate({
                    mtls: { ...config?.mtls, enabled: e.currentTarget.checked },
                  })
                }
              />
            </Group>
            <Switch
              label="Require Client Certificate"
              checked={config?.mtls?.require_client_cert ?? true}
              disabled={!config?.mtls?.enabled}
            />
          </Card>
        </Grid.Col>

        <Grid.Col span={{ base: 12, md: 6 }}>
          <Card withBorder p="md">
            <Group justify="space-between" mb="md">
              <Group>
                <ThemeIcon color="orange" variant="light">
                  <IconRoute size={20} />
                </ThemeIcon>
                <Text fw={500}>Distributed Tracing</Text>
              </Group>
              <Switch
                checked={config?.tracing?.enabled ?? false}
                onChange={(e) =>
                  updateMutation.mutate({
                    tracing: { ...config?.tracing, enabled: e.currentTarget.checked },
                  })
                }
              />
            </Group>
            <Stack gap="sm">
              <NumberInput
                label="Sampling Rate"
                value={(config?.tracing?.sampling_rate ?? 0.1) * 100}
                min={1}
                max={100}
                suffix="%"
                disabled={!config?.tracing?.enabled}
              />
              <TextInput
                label="OTLP Endpoint"
                value={config?.tracing?.endpoint ?? ''}
                placeholder="http://otel-collector:4317"
                disabled={!config?.tracing?.enabled}
              />
            </Stack>
          </Card>
        </Grid.Col>
      </Grid>
    </Stack>
  );
}

// Main Component
export function SecurityFeaturesPage() {
  const [activeTab, setActiveTab] = useState<string | null>('analytics');

  return (
    <div>
      <Group justify="space-between" mb="lg">
        <div>
          <Title order={2}>Security Features</Title>
          <Text c="dimmed" size="sm">
            Monitor and manage security features including access analytics, encryption, and tracing
          </Text>
        </div>
      </Group>

      <Tabs value={activeTab} onChange={setActiveTab}>
        <Tabs.List>
          <Tabs.Tab value="analytics" leftSection={<IconShield size={16} />}>
            Access Analytics
          </Tabs.Tab>
          <Tabs.Tab value="ratelimiting" leftSection={<IconFlame size={16} />}>
            Rate Limiting
          </Tabs.Tab>
          <Tabs.Tab value="keys" leftSection={<IconKey size={16} />}>
            Key Rotation
          </Tabs.Tab>
          <Tabs.Tab value="mtls" leftSection={<IconLock size={16} />}>
            mTLS Certificates
          </Tabs.Tab>
          <Tabs.Tab value="tracing" leftSection={<IconRoute size={16} />}>
            Distributed Tracing
          </Tabs.Tab>
          <Tabs.Tab value="config" leftSection={<IconSettings size={16} />}>
            Configuration
          </Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value="analytics" pt="md">
          <AccessAnalyticsTab />
        </Tabs.Panel>

        <Tabs.Panel value="ratelimiting" pt="md">
          <RateLimitingTab />
        </Tabs.Panel>

        <Tabs.Panel value="keys" pt="md">
          <KeyRotationTab />
        </Tabs.Panel>

        <Tabs.Panel value="mtls" pt="md">
          <MTLSTab />
        </Tabs.Panel>

        <Tabs.Panel value="tracing" pt="md">
          <TracingTab />
        </Tabs.Panel>

        <Tabs.Panel value="config" pt="md">
          <ConfigurationTab />
        </Tabs.Panel>
      </Tabs>
    </div>
  );
}
