import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  Title,
  Button,
  Table,
  Group,
  Text,
  ActionIcon,
  Modal,
  TextInput,
  Textarea,
  Stack,
  Badge,
  Skeleton,
  Paper,
  Menu,
  Switch,
  Select,
  NumberInput,
  Divider,
  Grid,
  Card,
  Tabs,
  Code,
  Checkbox,
  Alert,
} from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import {
  IconPlus,
  IconTrash,
  IconDotsVertical,
  IconEdit,
  IconEye,
  IconToggleLeft,
  IconToggleRight,
  IconClock,
  IconChartBar,
  IconFilter,
  IconAlertCircle,
  IconDatabase,
} from '@tabler/icons-react';
import { adminApi, TieringPolicy, TieringTrigger, TieringAction } from '../api/client';

const POLICY_TYPES = [
  { value: 'scheduled', label: 'Scheduled' },
  { value: 'realtime', label: 'Real-time' },
  { value: 'threshold', label: 'Threshold-based' },
  { value: 's3_lifecycle', label: 'S3 Lifecycle' },
];

const SCOPE_TYPES = [
  { value: 'global', label: 'Global' },
  { value: 'bucket', label: 'Bucket Pattern' },
  { value: 'prefix', label: 'Prefix Pattern' },
];

const TRIGGER_TYPES = [
  { value: 'age', label: 'Age-based' },
  { value: 'access', label: 'Access-based' },
  { value: 'capacity', label: 'Capacity-based' },
];

const ACTION_TYPES = [
  { value: 'transition', label: 'Transition to Tier' },
  { value: 'delete', label: 'Delete Object' },
];

const STORAGE_TIERS = [
  { value: 'hot', label: 'Hot (High Performance)' },
  { value: 'warm', label: 'Warm (Balanced)' },
  { value: 'cold', label: 'Cold (Archive)' },
  { value: 'glacier', label: 'Glacier (Deep Archive)' },
];

export function TieringPoliciesPage() {
  const [createModalOpen, setCreateModalOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<TieringPolicy | null>(null);
  const [viewTarget, setViewTarget] = useState<TieringPolicy | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<TieringPolicy | null>(null);
  const [statsTarget, setStatsTarget] = useState<TieringPolicy | null>(null);

  // Filters
  const [typeFilter, setTypeFilter] = useState<string | null>(null);
  const [scopeFilter, setScopeFilter] = useState<string | null>(null);
  const [enabledFilter, setEnabledFilter] = useState<boolean | null>(null);

  const queryClient = useQueryClient();

  const { data: policies, isLoading } = useQuery({
    queryKey: ['tiering-policies', typeFilter, scopeFilter, enabledFilter],
    queryFn: () =>
      adminApi
        .listTieringPolicies({
          type: typeFilter || undefined,
          scope: scopeFilter || undefined,
          enabled: enabledFilter !== null ? enabledFilter : undefined,
        })
        .then((res) => {
          // API returns { policies: [...], total_count: N }
          const data = res.data as { policies: TieringPolicy[]; total_count: number } | TieringPolicy[];
          return Array.isArray(data) ? data : data.policies || [];
        }),
  });

  const createMutation = useMutation({
    mutationFn: (data: Omit<TieringPolicy, 'id' | 'created_at' | 'updated_at' | 'last_run' | 'stats'>) =>
      adminApi.createTieringPolicy(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tiering-policies'] });
      setCreateModalOpen(false);
      createForm.reset();
      notifications.show({
        title: 'Policy created',
        message: 'The tiering policy has been created successfully',
        color: 'green',
      });
    },
    onError: (error: Error & { response?: { data?: { error?: string } } }) => {
      notifications.show({
        title: 'Error',
        message: error.response?.data?.error || 'Failed to create tiering policy',
        color: 'red',
      });
    },
  });

  const updateMutation = useMutation({
    mutationFn: ({
      id,
      data,
    }: {
      id: string;
      data: Partial<Omit<TieringPolicy, 'id' | 'created_at' | 'updated_at' | 'last_run' | 'stats'>>;
    }) => adminApi.updateTieringPolicy(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tiering-policies'] });
      setEditTarget(null);
      notifications.show({
        title: 'Policy updated',
        message: 'The tiering policy has been updated successfully',
        color: 'green',
      });
    },
    onError: (error: Error & { response?: { data?: { error?: string } } }) => {
      notifications.show({
        title: 'Error',
        message: error.response?.data?.error || 'Failed to update tiering policy',
        color: 'red',
      });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => adminApi.deleteTieringPolicy(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tiering-policies'] });
      setDeleteTarget(null);
      notifications.show({
        title: 'Policy deleted',
        message: 'The tiering policy has been deleted successfully',
        color: 'green',
      });
    },
    onError: (error: Error & { response?: { data?: { error?: string } } }) => {
      notifications.show({
        title: 'Error',
        message: error.response?.data?.error || 'Failed to delete tiering policy',
        color: 'red',
      });
    },
  });

  const toggleMutation = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      enabled ? adminApi.enableTieringPolicy(id) : adminApi.disableTieringPolicy(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tiering-policies'] });
      notifications.show({
        title: 'Policy updated',
        message: 'The policy status has been updated',
        color: 'green',
      });
    },
    onError: () => {
      notifications.show({
        title: 'Error',
        message: 'Failed to update policy status',
        color: 'red',
      });
    },
  });

  const createForm = useForm({
    initialValues: {
      name: '',
      description: '',
      type: 'scheduled' as TieringPolicy['type'],
      scope: 'global' as TieringPolicy['scope'],
      bucket_pattern: '',
      prefix_pattern: '',
      enabled: true,
      cron_expression: '0 0 * * *',

      // Triggers
      trigger_type: 'age' as TieringTrigger['type'],
      age_days: 30,
      access_count: 0,
      access_days: 0,
      capacity_percent: 80,

      // Actions
      action_type: 'transition' as TieringAction['type'],
      target_tier: 'warm',
      notify_url: '',

      // Schedule
      maintenance_windows: [] as string[],
      blackout_windows: [] as string[],

      // Advanced Options
      rate_limit: 100,
      anti_thrash_hours: 24,
      distributed_execution: false,
    },
    validate: {
      name: (value) => {
        if (value.length < 1) return 'Name is required';
        if (!/^[a-zA-Z0-9_-]+$/.test(value)) {
          return 'Name can only contain letters, numbers, hyphens, and underscores';
        }
        return null;
      },
      bucket_pattern: (value, values) => {
        if (values.scope === 'bucket' && !value) {
          return 'Bucket pattern is required for bucket scope';
        }
        return null;
      },
      prefix_pattern: (value, values) => {
        if (values.scope === 'prefix' && !value) {
          return 'Prefix pattern is required for prefix scope';
        }
        return null;
      },
      target_tier: (value, values) => {
        if (values.action_type === 'transition' && !value) {
          return 'Target tier is required for transition action';
        }
        return null;
      },
    },
  });

  const editForm = useForm({
    initialValues: createForm.values,
    validate: {
      bucket_pattern: (value, values) => {
        if (values.scope === 'bucket' && !value) {
          return 'Bucket pattern is required for bucket scope';
        }
        return null;
      },
      prefix_pattern: (value, values) => {
        if (values.scope === 'prefix' && !value) {
          return 'Prefix pattern is required for prefix scope';
        }
        return null;
      },
      target_tier: (value, values) => {
        if (values.action_type === 'transition' && !value) {
          return 'Target tier is required for transition action';
        }
        return null;
      },
    },
  });

  const handleEdit = (policy: TieringPolicy) => {
    const trigger = policy.triggers[0] || { type: 'age' };
    const action = policy.actions[0] || { type: 'transition' };

    editForm.setValues({
      name: policy.name,
      description: policy.description,
      type: policy.type,
      scope: policy.scope,
      bucket_pattern: policy.bucket_pattern || '',
      prefix_pattern: policy.prefix_pattern || '',
      enabled: policy.enabled,
      cron_expression: policy.cron_expression || '0 0 * * *',

      trigger_type: trigger.type,
      age_days: trigger.age_days || 30,
      access_count: trigger.access_count || 0,
      access_days: trigger.access_days || 0,
      capacity_percent: trigger.capacity_percent || 80,

      action_type: action.type,
      target_tier: action.target_tier || 'warm',
      notify_url: action.notify_url || '',

      maintenance_windows: policy.schedule?.maintenance_windows || [],
      blackout_windows: policy.schedule?.blackout_windows || [],

      rate_limit: policy.advanced_options?.rate_limit || 100,
      anti_thrash_hours: policy.advanced_options?.anti_thrash_hours || 24,
      distributed_execution: policy.advanced_options?.distributed_execution || false,
    });
    setEditTarget(policy);
  };

  const handleView = (policy: TieringPolicy) => {
    setViewTarget(policy);
  };

  const handleViewStats = (policy: TieringPolicy) => {
    setStatsTarget(policy);
  };

  const buildPolicyFromForm = (values: typeof createForm.values) => {
    const trigger: TieringTrigger = {
      type: values.trigger_type,
    };

    if (values.trigger_type === 'age') {
      trigger.age_days = values.age_days;
    } else if (values.trigger_type === 'access') {
      trigger.access_count = values.access_count;
      trigger.access_days = values.access_days;
    } else if (values.trigger_type === 'capacity') {
      trigger.capacity_percent = values.capacity_percent;
    }

    const action: TieringAction = {
      type: values.action_type,
    };

    if (values.action_type === 'transition') {
      action.target_tier = values.target_tier;
    }

    if (values.notify_url) {
      action.notify_url = values.notify_url;
    }

    return {
      name: values.name,
      description: values.description,
      type: values.type,
      scope: values.scope,
      bucket_pattern: values.scope === 'bucket' ? values.bucket_pattern : undefined,
      prefix_pattern: values.scope === 'prefix' ? values.prefix_pattern : undefined,
      enabled: values.enabled,
      triggers: [trigger],
      actions: [action],
      cron_expression: values.type === 'scheduled' ? values.cron_expression : undefined,
      schedule: values.maintenance_windows.length || values.blackout_windows.length
        ? {
            maintenance_windows: values.maintenance_windows.length ? values.maintenance_windows : undefined,
            blackout_windows: values.blackout_windows.length ? values.blackout_windows : undefined,
          }
        : undefined,
      advanced_options: {
        rate_limit: values.rate_limit,
        anti_thrash_hours: values.anti_thrash_hours,
        distributed_execution: values.distributed_execution,
      },
    };
  };

  const getStatusBadge = (enabled: boolean) => {
    return (
      <Badge color={enabled ? 'green' : 'gray'} variant="light">
        {enabled ? 'Enabled' : 'Disabled'}
      </Badge>
    );
  };

  const getScopeBadge = (policy: TieringPolicy) => {
    if (policy.scope === 'global') {
      return <Badge variant="light">Global</Badge>;
    } else if (policy.scope === 'bucket') {
      return (
        <Badge variant="light" color="blue">
          Bucket: {policy.bucket_pattern}
        </Badge>
      );
    } else {
      return (
        <Badge variant="light" color="cyan">
          Prefix: {policy.prefix_pattern}
        </Badge>
      );
    }
  };

  const filteredPolicies = policies || [];

  return (
    <div>
      <Group justify="space-between" mb="lg">
        <Group>
          <IconDatabase size={28} color="var(--mantine-color-blue-6)" />
          <Title order={2}>Tiering Policies</Title>
        </Group>
        <Button leftSection={<IconPlus size={16} />} onClick={() => setCreateModalOpen(true)}>
          Create Policy
        </Button>
      </Group>

      {/* Filters */}
      <Paper withBorder p="md" mb="md">
        <Group>
          <IconFilter size={20} />
          <Text fw={500}>Filters</Text>
        </Group>
        <Grid mt="sm">
          <Grid.Col span={{ base: 12, sm: 4 }}>
            <Select
              label="Type"
              placeholder="All types"
              data={POLICY_TYPES}
              value={typeFilter}
              onChange={setTypeFilter}
              clearable
            />
          </Grid.Col>
          <Grid.Col span={{ base: 12, sm: 4 }}>
            <Select
              label="Scope"
              placeholder="All scopes"
              data={SCOPE_TYPES}
              value={scopeFilter}
              onChange={setScopeFilter}
              clearable
            />
          </Grid.Col>
          <Grid.Col span={{ base: 12, sm: 4 }}>
            <Select
              label="Status"
              placeholder="All statuses"
              data={[
                { value: 'true', label: 'Enabled' },
                { value: 'false', label: 'Disabled' },
              ]}
              value={enabledFilter !== null ? String(enabledFilter) : null}
              onChange={(value) => setEnabledFilter(value ? value === 'true' : null)}
              clearable
            />
          </Grid.Col>
        </Grid>
      </Paper>

      {/* Policies Table */}
      <Paper withBorder>
        {isLoading ? (
          <Stack p="md">
            <Skeleton height={40} />
            <Skeleton height={40} />
            <Skeleton height={40} />
          </Stack>
        ) : filteredPolicies.length ? (
          <Table striped highlightOnHover>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Name</Table.Th>
                <Table.Th>Type</Table.Th>
                <Table.Th>Scope</Table.Th>
                <Table.Th>Status</Table.Th>
                <Table.Th>Last Run</Table.Th>
                <Table.Th style={{ width: 80 }}>Actions</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {filteredPolicies.map((policy: TieringPolicy) => (
                <Table.Tr key={policy.id}>
                  <Table.Td>
                    <div>
                      <Text fw={500}>{policy.name}</Text>
                      <Text size="sm" c="dimmed" lineClamp={1}>
                        {policy.description || '-'}
                      </Text>
                    </div>
                  </Table.Td>
                  <Table.Td>
                    <Badge variant="light" color="blue">
                      {POLICY_TYPES.find((t) => t.value === policy.type)?.label || policy.type}
                    </Badge>
                  </Table.Td>
                  <Table.Td>{getScopeBadge(policy)}</Table.Td>
                  <Table.Td>
                    <Group gap={8}>
                      {getStatusBadge(policy.enabled)}
                      <ActionIcon
                        variant="subtle"
                        color={policy.enabled ? 'gray' : 'green'}
                        onClick={() =>
                          toggleMutation.mutate({ id: policy.id, enabled: !policy.enabled })
                        }
                        loading={toggleMutation.isPending}
                      >
                        {policy.enabled ? (
                          <IconToggleRight size={20} />
                        ) : (
                          <IconToggleLeft size={20} />
                        )}
                      </ActionIcon>
                    </Group>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm" c="dimmed">
                      {policy.last_run ? new Date(policy.last_run).toLocaleString() : 'Never'}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Menu shadow="md" width={150}>
                      <Menu.Target>
                        <ActionIcon variant="subtle">
                          <IconDotsVertical size={16} />
                        </ActionIcon>
                      </Menu.Target>
                      <Menu.Dropdown>
                        <Menu.Item
                          leftSection={<IconEye size={14} />}
                          onClick={() => handleView(policy)}
                        >
                          View
                        </Menu.Item>
                        <Menu.Item
                          leftSection={<IconChartBar size={14} />}
                          onClick={() => handleViewStats(policy)}
                        >
                          Statistics
                        </Menu.Item>
                        <Menu.Item
                          leftSection={<IconEdit size={14} />}
                          onClick={() => handleEdit(policy)}
                        >
                          Edit
                        </Menu.Item>
                        <Menu.Divider />
                        <Menu.Item
                          color="red"
                          leftSection={<IconTrash size={14} />}
                          onClick={() => setDeleteTarget(policy)}
                        >
                          Delete
                        </Menu.Item>
                      </Menu.Dropdown>
                    </Menu>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        ) : (
          <Text c="dimmed" ta="center" p="xl">
            No tiering policies found. Create your first policy to get started.
          </Text>
        )}
      </Paper>

      {/* Create Policy Modal */}
      <Modal
        opened={createModalOpen}
        onClose={() => setCreateModalOpen(false)}
        title="Create Tiering Policy"
        size="xl"
      >
        <form
          onSubmit={createForm.onSubmit((values) =>
            createMutation.mutate(buildPolicyFromForm(values))
          )}
        >
          <Stack>
            <TextInput
              label="Policy Name"
              placeholder="hot-to-warm-30days"
              required
              description="Alphanumeric characters, hyphens, and underscores only"
              {...createForm.getInputProps('name')}
            />
            <Textarea
              label="Description"
              placeholder="Transition objects older than 30 days to warm storage"
              minRows={2}
              {...createForm.getInputProps('description')}
            />

            <Divider label="Configuration" labelPosition="left" />

            <Grid>
              <Grid.Col span={6}>
                <Select
                  label="Policy Type"
                  required
                  data={POLICY_TYPES}
                  {...createForm.getInputProps('type')}
                />
              </Grid.Col>
              <Grid.Col span={6}>
                <Select
                  label="Scope"
                  required
                  data={SCOPE_TYPES}
                  {...createForm.getInputProps('scope')}
                />
              </Grid.Col>
            </Grid>

            {createForm.values.scope === 'bucket' && (
              <TextInput
                label="Bucket Pattern"
                placeholder="my-bucket-* or exact-bucket-name"
                required
                {...createForm.getInputProps('bucket_pattern')}
              />
            )}

            {createForm.values.scope === 'prefix' && (
              <TextInput
                label="Prefix Pattern"
                placeholder="logs/* or data/archive/*"
                required
                {...createForm.getInputProps('prefix_pattern')}
              />
            )}

            {createForm.values.type === 'scheduled' && (
              <TextInput
                label="Cron Expression"
                placeholder="0 0 * * *"
                description="Run schedule in cron format (e.g., 0 0 * * * for daily at midnight)"
                {...createForm.getInputProps('cron_expression')}
              />
            )}

            <Divider label="Trigger Configuration" labelPosition="left" />

            <Select
              label="Trigger Type"
              required
              data={TRIGGER_TYPES}
              {...createForm.getInputProps('trigger_type')}
            />

            {createForm.values.trigger_type === 'age' && (
              <NumberInput
                label="Age (days)"
                description="Trigger when objects are older than this many days"
                min={1}
                required
                {...createForm.getInputProps('age_days')}
              />
            )}

            {createForm.values.trigger_type === 'access' && (
              <Grid>
                <Grid.Col span={6}>
                  <NumberInput
                    label="Access Count"
                    description="Minimum number of accesses"
                    min={0}
                    {...createForm.getInputProps('access_count')}
                  />
                </Grid.Col>
                <Grid.Col span={6}>
                  <NumberInput
                    label="Access Period (days)"
                    description="Within this many days"
                    min={1}
                    {...createForm.getInputProps('access_days')}
                  />
                </Grid.Col>
              </Grid>
            )}

            {createForm.values.trigger_type === 'capacity' && (
              <NumberInput
                label="Capacity Threshold (%)"
                description="Trigger when storage reaches this percentage"
                min={1}
                max={100}
                required
                {...createForm.getInputProps('capacity_percent')}
              />
            )}

            <Divider label="Action Configuration" labelPosition="left" />

            <Select
              label="Action Type"
              required
              data={ACTION_TYPES}
              {...createForm.getInputProps('action_type')}
            />

            {createForm.values.action_type === 'transition' && (
              <Select
                label="Target Storage Tier"
                required
                data={STORAGE_TIERS}
                {...createForm.getInputProps('target_tier')}
              />
            )}

            {createForm.values.action_type === 'delete' && (
              <Alert icon={<IconAlertCircle size={16} />} color="red">
                This action will permanently delete objects. Use with caution!
              </Alert>
            )}

            <TextInput
              label="Notification Webhook URL"
              placeholder="https://example.com/webhook"
              description="Optional URL to notify when actions are performed"
              {...createForm.getInputProps('notify_url')}
            />

            <Divider label="Scheduling" labelPosition="left" />

            <Textarea
              label="Maintenance Windows"
              placeholder="02:00-04:00&#10;14:00-16:00"
              description="Time windows when policy can run (one per line)"
              minRows={2}
              value={createForm.values.maintenance_windows.join('\n')}
              onChange={(e) =>
                createForm.setFieldValue(
                  'maintenance_windows',
                  e.target.value.split('\n').filter(Boolean)
                )
              }
            />

            <Textarea
              label="Blackout Windows"
              placeholder="09:00-17:00&#10;18:00-22:00"
              description="Time windows when policy should NOT run (one per line)"
              minRows={2}
              value={createForm.values.blackout_windows.join('\n')}
              onChange={(e) =>
                createForm.setFieldValue(
                  'blackout_windows',
                  e.target.value.split('\n').filter(Boolean)
                )
              }
            />

            <Divider label="Advanced Options" labelPosition="left" />

            <Grid>
              <Grid.Col span={6}>
                <NumberInput
                  label="Rate Limit (ops/sec)"
                  description="Maximum operations per second"
                  min={1}
                  {...createForm.getInputProps('rate_limit')}
                />
              </Grid.Col>
              <Grid.Col span={6}>
                <NumberInput
                  label="Anti-thrash Period (hours)"
                  description="Minimum time before re-tiering same object"
                  min={1}
                  {...createForm.getInputProps('anti_thrash_hours')}
                />
              </Grid.Col>
            </Grid>

            <Checkbox
              label="Enable distributed execution across cluster nodes"
              {...createForm.getInputProps('distributed_execution', { type: 'checkbox' })}
            />

            <Switch
              label="Enable Policy"
              {...createForm.getInputProps('enabled', { type: 'checkbox' })}
            />

            <Group justify="flex-end">
              <Button variant="default" onClick={() => setCreateModalOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" loading={createMutation.isPending}>
                Create
              </Button>
            </Group>
          </Stack>
        </form>
      </Modal>

      {/* Edit Policy Modal */}
      <Modal
        opened={editTarget !== null}
        onClose={() => setEditTarget(null)}
        title={`Edit Policy: ${editTarget?.name}`}
        size="xl"
      >
        <form
          onSubmit={editForm.onSubmit((values) =>
            updateMutation.mutate({ id: editTarget!.id, data: buildPolicyFromForm(values) })
          )}
        >
          <Stack>
            <Textarea
              label="Description"
              placeholder="Transition objects older than 30 days to warm storage"
              minRows={2}
              {...editForm.getInputProps('description')}
            />

            <Divider label="Configuration" labelPosition="left" />

            <Grid>
              <Grid.Col span={6}>
                <Select
                  label="Policy Type"
                  required
                  data={POLICY_TYPES}
                  {...editForm.getInputProps('type')}
                />
              </Grid.Col>
              <Grid.Col span={6}>
                <Select
                  label="Scope"
                  required
                  data={SCOPE_TYPES}
                  {...editForm.getInputProps('scope')}
                />
              </Grid.Col>
            </Grid>

            {editForm.values.scope === 'bucket' && (
              <TextInput
                label="Bucket Pattern"
                placeholder="my-bucket-* or exact-bucket-name"
                required
                {...editForm.getInputProps('bucket_pattern')}
              />
            )}

            {editForm.values.scope === 'prefix' && (
              <TextInput
                label="Prefix Pattern"
                placeholder="logs/* or data/archive/*"
                required
                {...editForm.getInputProps('prefix_pattern')}
              />
            )}

            {editForm.values.type === 'scheduled' && (
              <TextInput
                label="Cron Expression"
                placeholder="0 0 * * *"
                description="Run schedule in cron format"
                {...editForm.getInputProps('cron_expression')}
              />
            )}

            <Divider label="Trigger Configuration" labelPosition="left" />

            <Select
              label="Trigger Type"
              required
              data={TRIGGER_TYPES}
              {...editForm.getInputProps('trigger_type')}
            />

            {editForm.values.trigger_type === 'age' && (
              <NumberInput
                label="Age (days)"
                description="Trigger when objects are older than this many days"
                min={1}
                required
                {...editForm.getInputProps('age_days')}
              />
            )}

            {editForm.values.trigger_type === 'access' && (
              <Grid>
                <Grid.Col span={6}>
                  <NumberInput
                    label="Access Count"
                    description="Minimum number of accesses"
                    min={0}
                    {...editForm.getInputProps('access_count')}
                  />
                </Grid.Col>
                <Grid.Col span={6}>
                  <NumberInput
                    label="Access Period (days)"
                    description="Within this many days"
                    min={1}
                    {...editForm.getInputProps('access_days')}
                  />
                </Grid.Col>
              </Grid>
            )}

            {editForm.values.trigger_type === 'capacity' && (
              <NumberInput
                label="Capacity Threshold (%)"
                description="Trigger when storage reaches this percentage"
                min={1}
                max={100}
                required
                {...editForm.getInputProps('capacity_percent')}
              />
            )}

            <Divider label="Action Configuration" labelPosition="left" />

            <Select
              label="Action Type"
              required
              data={ACTION_TYPES}
              {...editForm.getInputProps('action_type')}
            />

            {editForm.values.action_type === 'transition' && (
              <Select
                label="Target Storage Tier"
                required
                data={STORAGE_TIERS}
                {...editForm.getInputProps('target_tier')}
              />
            )}

            <TextInput
              label="Notification Webhook URL"
              placeholder="https://example.com/webhook"
              description="Optional URL to notify when actions are performed"
              {...editForm.getInputProps('notify_url')}
            />

            <Divider label="Advanced Options" labelPosition="left" />

            <Grid>
              <Grid.Col span={6}>
                <NumberInput
                  label="Rate Limit (ops/sec)"
                  description="Maximum operations per second"
                  min={1}
                  {...editForm.getInputProps('rate_limit')}
                />
              </Grid.Col>
              <Grid.Col span={6}>
                <NumberInput
                  label="Anti-thrash Period (hours)"
                  description="Minimum time before re-tiering same object"
                  min={1}
                  {...editForm.getInputProps('anti_thrash_hours')}
                />
              </Grid.Col>
            </Grid>

            <Checkbox
              label="Enable distributed execution across cluster nodes"
              {...editForm.getInputProps('distributed_execution', { type: 'checkbox' })}
            />

            <Switch
              label="Enable Policy"
              {...editForm.getInputProps('enabled', { type: 'checkbox' })}
            />

            <Group justify="flex-end">
              <Button variant="default" onClick={() => setEditTarget(null)}>
                Cancel
              </Button>
              <Button type="submit" loading={updateMutation.isPending}>
                Save
              </Button>
            </Group>
          </Stack>
        </form>
      </Modal>

      {/* View Policy Modal */}
      <Modal
        opened={viewTarget !== null}
        onClose={() => setViewTarget(null)}
        title={`Policy: ${viewTarget?.name}`}
        size="lg"
      >
        {viewTarget && (
          <Tabs defaultValue="details">
            <Tabs.List>
              <Tabs.Tab value="details">Details</Tabs.Tab>
              <Tabs.Tab value="configuration">Configuration</Tabs.Tab>
            </Tabs.List>

            <Tabs.Panel value="details" pt="md">
              <Stack>
                <Group>
                  <Text size="sm" c="dimmed" w={150}>
                    Name:
                  </Text>
                  <Text size="sm" fw={500}>
                    {viewTarget.name}
                  </Text>
                </Group>
                <Group>
                  <Text size="sm" c="dimmed" w={150}>
                    Description:
                  </Text>
                  <Text size="sm">{viewTarget.description || '-'}</Text>
                </Group>
                <Group>
                  <Text size="sm" c="dimmed" w={150}>
                    Type:
                  </Text>
                  <Badge variant="light" color="blue">
                    {POLICY_TYPES.find((t) => t.value === viewTarget.type)?.label}
                  </Badge>
                </Group>
                <Group>
                  <Text size="sm" c="dimmed" w={150}>
                    Scope:
                  </Text>
                  {getScopeBadge(viewTarget)}
                </Group>
                <Group>
                  <Text size="sm" c="dimmed" w={150}>
                    Status:
                  </Text>
                  {getStatusBadge(viewTarget.enabled)}
                </Group>
                <Group>
                  <Text size="sm" c="dimmed" w={150}>
                    Created:
                  </Text>
                  <Text size="sm">{new Date(viewTarget.created_at).toLocaleString()}</Text>
                </Group>
                <Group>
                  <Text size="sm" c="dimmed" w={150}>
                    Last Updated:
                  </Text>
                  <Text size="sm">{new Date(viewTarget.updated_at).toLocaleString()}</Text>
                </Group>
                <Group>
                  <Text size="sm" c="dimmed" w={150}>
                    Last Run:
                  </Text>
                  <Text size="sm">
                    {viewTarget.last_run ? new Date(viewTarget.last_run).toLocaleString() : 'Never'}
                  </Text>
                </Group>
              </Stack>
            </Tabs.Panel>

            <Tabs.Panel value="configuration" pt="md">
              <Stack>
                <div>
                  <Text size="sm" fw={500} mb="xs">
                    Triggers
                  </Text>
                  <Code block>
                    {JSON.stringify(viewTarget.triggers, null, 2)}
                  </Code>
                </div>
                <div>
                  <Text size="sm" fw={500} mb="xs">
                    Actions
                  </Text>
                  <Code block>
                    {JSON.stringify(viewTarget.actions, null, 2)}
                  </Code>
                </div>
                {viewTarget.schedule && (
                  <div>
                    <Text size="sm" fw={500} mb="xs">
                      Schedule
                    </Text>
                    <Code block>
                      {JSON.stringify(viewTarget.schedule, null, 2)}
                    </Code>
                  </div>
                )}
                {viewTarget.advanced_options && (
                  <div>
                    <Text size="sm" fw={500} mb="xs">
                      Advanced Options
                    </Text>
                    <Code block>
                      {JSON.stringify(viewTarget.advanced_options, null, 2)}
                    </Code>
                  </div>
                )}
              </Stack>
            </Tabs.Panel>
          </Tabs>
        )}
      </Modal>

      {/* Statistics Modal */}
      <Modal
        opened={statsTarget !== null}
        onClose={() => setStatsTarget(null)}
        title={`Statistics: ${statsTarget?.name}`}
        size="lg"
      >
        {statsTarget && (
          <Stack>
            <Card withBorder>
              <Group justify="space-between">
                <div>
                  <Text size="sm" c="dimmed">
                    Objects Affected
                  </Text>
                  <Text size="xl" fw={700}>
                    {statsTarget.stats?.objects_affected?.toLocaleString() || 0}
                  </Text>
                </div>
                <IconDatabase size={40} color="var(--mantine-color-blue-6)" />
              </Group>
            </Card>

            <Card withBorder>
              <Group justify="space-between">
                <div>
                  <Text size="sm" c="dimmed">
                    Transitions Performed
                  </Text>
                  <Text size="xl" fw={700}>
                    {statsTarget.stats?.transitions_performed?.toLocaleString() || 0}
                  </Text>
                </div>
                <IconChartBar size={40} color="var(--mantine-color-green-6)" />
              </Group>
            </Card>

            <Card withBorder>
              <Group justify="space-between">
                <div>
                  <Text size="sm" c="dimmed">
                    Last Execution
                  </Text>
                  <Text size="lg" fw={500}>
                    {statsTarget.stats?.last_execution_time
                      ? new Date(statsTarget.stats.last_execution_time).toLocaleString()
                      : 'Never'}
                  </Text>
                </div>
                <IconClock size={40} color="var(--mantine-color-orange-6)" />
              </Group>
            </Card>

            <Grid>
              <Grid.Col span={6}>
                <Card withBorder>
                  <Text size="sm" c="dimmed">
                    Successful Operations
                  </Text>
                  <Text size="xl" fw={700} c="green">
                    {statsTarget.stats?.success_count?.toLocaleString() || 0}
                  </Text>
                </Card>
              </Grid.Col>
              <Grid.Col span={6}>
                <Card withBorder>
                  <Text size="sm" c="dimmed">
                    Failed Operations
                  </Text>
                  <Text size="xl" fw={700} c="red">
                    {statsTarget.stats?.failure_count?.toLocaleString() || 0}
                  </Text>
                </Card>
              </Grid.Col>
            </Grid>
          </Stack>
        )}
      </Modal>

      {/* Delete Confirmation Modal */}
      <Modal
        opened={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title="Delete Tiering Policy"
      >
        <Text mb="md">
          Are you sure you want to delete the tiering policy <strong>{deleteTarget?.name}</strong>?
          This action cannot be undone. The policy will stop running and no further tiering actions
          will be performed.
        </Text>
        <Group justify="flex-end">
          <Button variant="default" onClick={() => setDeleteTarget(null)}>
            Cancel
          </Button>
          <Button
            color="red"
            onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget.id)}
            loading={deleteMutation.isPending}
          >
            Delete
          </Button>
        </Group>
      </Modal>
    </div>
  );
}
