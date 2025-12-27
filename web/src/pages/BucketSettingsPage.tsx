import { useState, useMemo } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  Title,
  Tabs,
  Card,
  Group,
  Text,
  Switch,
  Button,
  Stack,
  TextInput,
  ActionIcon,
  Table,
  Badge,
  Modal,
  NumberInput,
  Select,
  Textarea,
  Paper,
  Alert,
  Loader,
  Center,
  Skeleton,
  Code,
} from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import {
  IconArrowLeft,
  IconVersions,
  IconClock,
  IconWorld,
  IconShieldLock,
  IconTags,
  IconPlus,
  IconTrash,
  IconCheck,
} from '@tabler/icons-react';
import { adminApi, LifecycleRule, CorsRule } from '../api/client';
import { PolicyEditor } from '../components/PolicyEditor';

export function BucketSettingsPage() {
  const { bucketName } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  // Versioning state
  const { data: versioningData, isLoading: versioningLoading } = useQuery({
    queryKey: ['bucket-versioning', bucketName],
    queryFn: () => adminApi.getBucketVersioning(bucketName!).then((res) => res.data),
  });

  const versioningMutation = useMutation({
    mutationFn: (enabled: boolean) => adminApi.setBucketVersioning(bucketName!, enabled),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket-versioning', bucketName] });
      notifications.show({
        title: 'Versioning updated',
        message: 'Bucket versioning settings have been updated',
        color: 'green',
      });
    },
    onError: () => {
      notifications.show({
        title: 'Error',
        message: 'Failed to update versioning settings',
        color: 'red',
      });
    },
  });

  // Lifecycle rules state
  const [lifecycleModalOpen, setLifecycleModalOpen] = useState(false);
  const [editingRule, setEditingRule] = useState<LifecycleRule | null>(null);

  const { data: lifecycleData, isLoading: lifecycleLoading } = useQuery({
    queryKey: ['bucket-lifecycle', bucketName],
    queryFn: () => adminApi.getBucketLifecycle(bucketName!).then((res) => res.data),
  });

  const lifecycleForm = useForm({
    initialValues: {
      id: '',
      prefix: '',
      enabled: true,
      expiration_days: 0,
      noncurrent_expiration_days: 0,
    },
    validate: {
      id: (value) => (value.length < 1 ? 'Rule ID is required' : null),
    },
  });

  const lifecycleMutation = useMutation({
    mutationFn: (rules: LifecycleRule[]) => adminApi.setBucketLifecycle(bucketName!, rules),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket-lifecycle', bucketName] });
      setLifecycleModalOpen(false);
      setEditingRule(null);
      lifecycleForm.reset();
      notifications.show({
        title: 'Lifecycle rules updated',
        message: 'Bucket lifecycle rules have been updated',
        color: 'green',
      });
    },
    onError: () => {
      notifications.show({
        title: 'Error',
        message: 'Failed to update lifecycle rules',
        color: 'red',
      });
    },
  });

  const deleteLifecycleRule = (ruleId: string) => {
    const currentRules = (lifecycleData?.rules || []) as LifecycleRule[];
    const newRules = currentRules.filter((r: LifecycleRule) => r.id !== ruleId);
    lifecycleMutation.mutate(newRules);
  };

  const saveLifecycleRule = (values: typeof lifecycleForm.values) => {
    const currentRules = (lifecycleData?.rules || []) as LifecycleRule[];
    const newRule: LifecycleRule = {
      id: values.id,
      prefix: values.prefix,
      enabled: values.enabled,
      expiration_days: values.expiration_days || undefined,
      noncurrent_expiration_days: values.noncurrent_expiration_days || undefined,
    };

    let newRules: LifecycleRule[];
    if (editingRule) {
      newRules = currentRules.map((r: LifecycleRule) =>
        r.id === editingRule.id ? newRule : r
      );
    } else {
      newRules = [...currentRules, newRule];
    }
    lifecycleMutation.mutate(newRules);
  };

  // CORS state
  const [corsModalOpen, setCorsModalOpen] = useState(false);
  const [, setEditingCorsRule] = useState<CorsRule | null>(null);

  const { data: corsData, isLoading: corsLoading } = useQuery({
    queryKey: ['bucket-cors', bucketName],
    queryFn: () => adminApi.getBucketCors(bucketName!).then((res) => res.data),
  });

  const corsForm = useForm({
    initialValues: {
      allowed_origins: '',
      allowed_methods: [] as string[],
      allowed_headers: '',
      expose_headers: '',
      max_age_seconds: 3600,
    },
  });

  const corsMutation = useMutation({
    mutationFn: (rules: CorsRule[]) => adminApi.setBucketCors(bucketName!, rules),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket-cors', bucketName] });
      setCorsModalOpen(false);
      setEditingCorsRule(null);
      corsForm.reset();
      notifications.show({
        title: 'CORS rules updated',
        message: 'Bucket CORS configuration has been updated',
        color: 'green',
      });
    },
    onError: () => {
      notifications.show({
        title: 'Error',
        message: 'Failed to update CORS configuration',
        color: 'red',
      });
    },
  });

  const saveCorsRule = (values: typeof corsForm.values) => {
    const newRule: CorsRule = {
      allowed_origins: values.allowed_origins.split('\n').filter(Boolean),
      allowed_methods: values.allowed_methods,
      allowed_headers: values.allowed_headers.split('\n').filter(Boolean),
      expose_headers: values.expose_headers ? values.expose_headers.split('\n').filter(Boolean) : undefined,
      max_age_seconds: values.max_age_seconds,
    };

    // For simplicity, just add or replace all rules
    corsMutation.mutate([newRule]);
  };

  // Bucket Policy state
  const [policyDocument, setPolicyDocument] = useState<string | null>(null);
  const [policyError, setPolicyError] = useState<string | null>(null);

  const { data: policyData, isLoading: policyLoading } = useQuery({
    queryKey: ['bucket-policy', bucketName],
    queryFn: () => adminApi.getBucketPolicy(bucketName!).then((res) => res.data as { policy?: string }),
  });

  // Derive formatted policy from server data, allowing user edits to override
  const effectivePolicyDocument = useMemo(() => {
    if (policyDocument !== null) {
      return policyDocument;
    }
    if (policyData?.policy) {
      try {
        return JSON.stringify(JSON.parse(policyData.policy), null, 2);
      } catch {
        return policyData.policy;
      }
    }
    return '';
  }, [policyDocument, policyData]);

  const policyMutation = useMutation({
    mutationFn: (policy: string) => adminApi.setBucketPolicy(bucketName!, policy),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket-policy', bucketName] });
      notifications.show({
        title: 'Bucket policy updated',
        message: 'Bucket policy has been saved',
        color: 'green',
      });
    },
    onError: (error: Error & { response?: { data?: { error?: string } } }) => {
      notifications.show({
        title: 'Error',
        message: error.response?.data?.error || 'Failed to update bucket policy',
        color: 'red',
      });
    },
  });

  const deletePolicyMutation = useMutation({
    mutationFn: () => adminApi.deleteBucketPolicy(bucketName!),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket-policy', bucketName] });
      setPolicyDocument('');
      notifications.show({
        title: 'Bucket policy deleted',
        message: 'Bucket policy has been removed',
        color: 'green',
      });
    },
    onError: () => {
      notifications.show({
        title: 'Error',
        message: 'Failed to delete bucket policy',
        color: 'red',
      });
    },
  });

  // Tags state
  const [newTagKey, setNewTagKey] = useState('');
  const [newTagValue, setNewTagValue] = useState('');

  const { data: tagsData, isLoading: tagsLoading } = useQuery({
    queryKey: ['bucket-tags', bucketName],
    queryFn: () => adminApi.getBucketTags(bucketName!).then((res) => res.data),
  });

  const tagsMutation = useMutation({
    mutationFn: (tags: Record<string, string>) => adminApi.setBucketTags(bucketName!, tags),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket-tags', bucketName] });
      setNewTagKey('');
      setNewTagValue('');
      notifications.show({
        title: 'Tags updated',
        message: 'Bucket tags have been updated',
        color: 'green',
      });
    },
    onError: () => {
      notifications.show({
        title: 'Error',
        message: 'Failed to update tags',
        color: 'red',
      });
    },
  });

  const addTag = () => {
    if (!newTagKey.trim()) return;
    const currentTags = (tagsData?.tags || {}) as Record<string, string>;
    tagsMutation.mutate({ ...currentTags, [newTagKey]: newTagValue });
  };

  const removeTag = (key: string) => {
    const currentTags = (tagsData?.tags || {}) as Record<string, string>;
    const newTags = { ...currentTags };
    delete newTags[key];
    tagsMutation.mutate(newTags);
  };

  return (
    <div>
      <Group mb="lg">
        <ActionIcon variant="subtle" size="lg" onClick={() => navigate(`/buckets/${bucketName}`)}>
          <IconArrowLeft size={20} />
        </ActionIcon>
        <Title order={2}>Settings: {bucketName}</Title>
      </Group>

      <Tabs defaultValue="versioning">
        <Tabs.List>
          <Tabs.Tab value="versioning" leftSection={<IconVersions size={16} />}>
            Versioning
          </Tabs.Tab>
          <Tabs.Tab value="lifecycle" leftSection={<IconClock size={16} />}>
            Lifecycle
          </Tabs.Tab>
          <Tabs.Tab value="cors" leftSection={<IconWorld size={16} />}>
            CORS
          </Tabs.Tab>
          <Tabs.Tab value="policy" leftSection={<IconShieldLock size={16} />}>
            Bucket Policy
          </Tabs.Tab>
          <Tabs.Tab value="tags" leftSection={<IconTags size={16} />}>
            Tags
          </Tabs.Tab>
        </Tabs.List>

        {/* Versioning Tab */}
        <Tabs.Panel value="versioning" pt="md">
          <Card withBorder>
            <Group justify="space-between">
              <div>
                <Text fw={500} size="lg">
                  Object Versioning
                </Text>
                <Text size="sm" c="dimmed">
                  Enable versioning to keep multiple versions of an object in the same bucket.
                </Text>
              </div>
              {versioningLoading ? (
                <Loader size="sm" />
              ) : (
                <Switch
                  checked={versioningData?.enabled || false}
                  onChange={(e) => versioningMutation.mutate(e.currentTarget.checked)}
                  disabled={versioningMutation.isPending}
                  size="lg"
                  onLabel="ON"
                  offLabel="OFF"
                />
              )}
            </Group>
            {versioningData?.enabled && (
              <Alert icon={<IconCheck size={16} />} color="green" mt="md">
                Versioning is enabled. All object uploads will create new versions.
              </Alert>
            )}
          </Card>
        </Tabs.Panel>

        {/* Lifecycle Tab */}
        <Tabs.Panel value="lifecycle" pt="md">
          <Card withBorder>
            <Group justify="space-between" mb="md">
              <div>
                <Text fw={500} size="lg">
                  Lifecycle Rules
                </Text>
                <Text size="sm" c="dimmed">
                  Configure rules to automatically manage object lifecycle.
                </Text>
              </div>
              <Button
                leftSection={<IconPlus size={16} />}
                onClick={() => {
                  lifecycleForm.reset();
                  setEditingRule(null);
                  setLifecycleModalOpen(true);
                }}
              >
                Add Rule
              </Button>
            </Group>

            {lifecycleLoading ? (
              <Center p="xl">
                <Loader />
              </Center>
            ) : (lifecycleData?.rules as LifecycleRule[])?.length ? (
              <Table>
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>Rule ID</Table.Th>
                    <Table.Th>Prefix</Table.Th>
                    <Table.Th>Status</Table.Th>
                    <Table.Th>Expiration</Table.Th>
                    <Table.Th>Actions</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {(lifecycleData?.rules as LifecycleRule[]).map((rule: LifecycleRule) => (
                    <Table.Tr key={rule.id}>
                      <Table.Td>
                        <Text fw={500}>{rule.id}</Text>
                      </Table.Td>
                      <Table.Td>
                        <Code>{rule.prefix || '*'}</Code>
                      </Table.Td>
                      <Table.Td>
                        <Badge color={rule.enabled ? 'green' : 'gray'}>
                          {rule.enabled ? 'Enabled' : 'Disabled'}
                        </Badge>
                      </Table.Td>
                      <Table.Td>
                        {rule.expiration_days ? `${rule.expiration_days} days` : '-'}
                      </Table.Td>
                      <Table.Td>
                        <ActionIcon
                          color="red"
                          variant="subtle"
                          onClick={() => deleteLifecycleRule(rule.id)}
                        >
                          <IconTrash size={16} />
                        </ActionIcon>
                      </Table.Td>
                    </Table.Tr>
                  ))}
                </Table.Tbody>
              </Table>
            ) : (
              <Text c="dimmed" ta="center" py="xl">
                No lifecycle rules configured
              </Text>
            )}
          </Card>
        </Tabs.Panel>

        {/* CORS Tab */}
        <Tabs.Panel value="cors" pt="md">
          <Card withBorder>
            <Group justify="space-between" mb="md">
              <div>
                <Text fw={500} size="lg">
                  CORS Configuration
                </Text>
                <Text size="sm" c="dimmed">
                  Configure Cross-Origin Resource Sharing rules for browser access.
                </Text>
              </div>
              <Button
                leftSection={<IconPlus size={16} />}
                onClick={() => {
                  corsForm.reset();
                  setEditingCorsRule(null);
                  setCorsModalOpen(true);
                }}
              >
                Configure CORS
              </Button>
            </Group>

            {corsLoading ? (
              <Center p="xl">
                <Loader />
              </Center>
            ) : (corsData?.rules as CorsRule[])?.length ? (
              <Stack>
                {(corsData?.rules as CorsRule[]).map((rule: CorsRule, index: number) => (
                  <Paper key={index} withBorder p="md">
                    <Group justify="space-between" mb="xs">
                      <Text fw={500}>Rule {index + 1}</Text>
                      <ActionIcon
                        color="red"
                        variant="subtle"
                        onClick={() => corsMutation.mutate([])}
                      >
                        <IconTrash size={16} />
                      </ActionIcon>
                    </Group>
                    <Stack gap="xs">
                      <Group>
                        <Text size="sm" c="dimmed" w={120}>
                          Origins:
                        </Text>
                        <Group gap={4}>
                          {rule.allowed_origins.map((o) => (
                            <Badge key={o} size="sm" variant="light">
                              {o}
                            </Badge>
                          ))}
                        </Group>
                      </Group>
                      <Group>
                        <Text size="sm" c="dimmed" w={120}>
                          Methods:
                        </Text>
                        <Group gap={4}>
                          {rule.allowed_methods.map((m) => (
                            <Badge key={m} size="sm" variant="light" color="blue">
                              {m}
                            </Badge>
                          ))}
                        </Group>
                      </Group>
                      <Group>
                        <Text size="sm" c="dimmed" w={120}>
                          Max Age:
                        </Text>
                        <Text size="sm">{rule.max_age_seconds || 0} seconds</Text>
                      </Group>
                    </Stack>
                  </Paper>
                ))}
              </Stack>
            ) : (
              <Text c="dimmed" ta="center" py="xl">
                No CORS rules configured
              </Text>
            )}
          </Card>
        </Tabs.Panel>

        {/* Bucket Policy Tab */}
        <Tabs.Panel value="policy" pt="md">
          <Card withBorder>
            <Stack>
              <div>
                <Text fw={500} size="lg">
                  Bucket Policy
                </Text>
                <Text size="sm" c="dimmed">
                  Define access control policies for this bucket using JSON policy documents.
                </Text>
              </div>

              {policyLoading ? (
                <Skeleton height={300} />
              ) : (
                <>
                  <PolicyEditor
                    value={effectivePolicyDocument}
                    onChange={setPolicyDocument}
                    error={policyError || undefined}
                  />
                  <Group justify="flex-end">
                    {policyData?.policy && (
                      <Button
                        variant="outline"
                        color="red"
                        onClick={() => deletePolicyMutation.mutate()}
                        loading={deletePolicyMutation.isPending}
                      >
                        Delete Policy
                      </Button>
                    )}
                    <Button
                      onClick={() => {
                        try {
                          JSON.parse(effectivePolicyDocument);
                          setPolicyError(null);
                          policyMutation.mutate(effectivePolicyDocument);
                        } catch {
                          setPolicyError('Invalid JSON');
                        }
                      }}
                      loading={policyMutation.isPending}
                      disabled={!effectivePolicyDocument.trim()}
                    >
                      Save Policy
                    </Button>
                  </Group>
                </>
              )}
            </Stack>
          </Card>
        </Tabs.Panel>

        {/* Tags Tab */}
        <Tabs.Panel value="tags" pt="md">
          <Card withBorder>
            <Stack>
              <div>
                <Text fw={500} size="lg">
                  Bucket Tags
                </Text>
                <Text size="sm" c="dimmed">
                  Add tags to organize and identify your bucket.
                </Text>
              </div>

              {tagsLoading ? (
                <Skeleton height={100} />
              ) : (
                <>
                  <Group>
                    <TextInput
                      placeholder="Key"
                      value={newTagKey}
                      onChange={(e) => setNewTagKey(e.target.value)}
                      style={{ flex: 1 }}
                    />
                    <TextInput
                      placeholder="Value"
                      value={newTagValue}
                      onChange={(e) => setNewTagValue(e.target.value)}
                      style={{ flex: 1 }}
                    />
                    <Button
                      leftSection={<IconPlus size={16} />}
                      onClick={addTag}
                      disabled={!newTagKey.trim()}
                      loading={tagsMutation.isPending}
                    >
                      Add
                    </Button>
                  </Group>

                  {Object.entries((tagsData?.tags || {}) as Record<string, string>).length > 0 ? (
                    <Table>
                      <Table.Thead>
                        <Table.Tr>
                          <Table.Th>Key</Table.Th>
                          <Table.Th>Value</Table.Th>
                          <Table.Th style={{ width: 60 }}>Actions</Table.Th>
                        </Table.Tr>
                      </Table.Thead>
                      <Table.Tbody>
                        {Object.entries((tagsData?.tags || {}) as Record<string, string>).map(
                          ([key, value]) => (
                            <Table.Tr key={key}>
                              <Table.Td>
                                <Code>{key}</Code>
                              </Table.Td>
                              <Table.Td>{value}</Table.Td>
                              <Table.Td>
                                <ActionIcon
                                  color="red"
                                  variant="subtle"
                                  onClick={() => removeTag(key)}
                                >
                                  <IconTrash size={16} />
                                </ActionIcon>
                              </Table.Td>
                            </Table.Tr>
                          )
                        )}
                      </Table.Tbody>
                    </Table>
                  ) : (
                    <Text c="dimmed" ta="center" py="md">
                      No tags configured
                    </Text>
                  )}
                </>
              )}
            </Stack>
          </Card>
        </Tabs.Panel>
      </Tabs>

      {/* Lifecycle Rule Modal */}
      <Modal
        opened={lifecycleModalOpen}
        onClose={() => setLifecycleModalOpen(false)}
        title={editingRule ? 'Edit Lifecycle Rule' : 'Add Lifecycle Rule'}
      >
        <form onSubmit={lifecycleForm.onSubmit(saveLifecycleRule)}>
          <Stack>
            <TextInput
              label="Rule ID"
              placeholder="my-rule"
              required
              {...lifecycleForm.getInputProps('id')}
            />
            <TextInput
              label="Prefix"
              placeholder="logs/"
              description="Apply rule to objects with this prefix (leave empty for all)"
              {...lifecycleForm.getInputProps('prefix')}
            />
            <Switch
              label="Enabled"
              {...lifecycleForm.getInputProps('enabled', { type: 'checkbox' })}
            />
            <NumberInput
              label="Expiration (days)"
              description="Delete objects after this many days (0 to disable)"
              min={0}
              {...lifecycleForm.getInputProps('expiration_days')}
            />
            <NumberInput
              label="Noncurrent Version Expiration (days)"
              description="Delete noncurrent versions after this many days"
              min={0}
              {...lifecycleForm.getInputProps('noncurrent_expiration_days')}
            />
            <Group justify="flex-end">
              <Button variant="default" onClick={() => setLifecycleModalOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" loading={lifecycleMutation.isPending}>
                Save
              </Button>
            </Group>
          </Stack>
        </form>
      </Modal>

      {/* CORS Modal */}
      <Modal
        opened={corsModalOpen}
        onClose={() => setCorsModalOpen(false)}
        title="Configure CORS"
        size="lg"
      >
        <form onSubmit={corsForm.onSubmit(saveCorsRule)}>
          <Stack>
            <Textarea
              label="Allowed Origins"
              placeholder="https://example.com&#10;https://app.example.com"
              description="One origin per line, or * for all"
              minRows={3}
              {...corsForm.getInputProps('allowed_origins')}
            />
            <Select
              label="Allowed Methods"
              placeholder="Select methods"
              data={['GET', 'PUT', 'POST', 'DELETE', 'HEAD']}
              multiple
              {...corsForm.getInputProps('allowed_methods')}
            />
            <Textarea
              label="Allowed Headers"
              placeholder="Content-Type&#10;Authorization"
              description="One header per line, or * for all"
              minRows={2}
              {...corsForm.getInputProps('allowed_headers')}
            />
            <Textarea
              label="Expose Headers"
              placeholder="x-amz-request-id"
              description="One header per line (optional)"
              minRows={2}
              {...corsForm.getInputProps('expose_headers')}
            />
            <NumberInput
              label="Max Age (seconds)"
              description="How long browsers should cache the preflight response"
              min={0}
              max={86400}
              {...corsForm.getInputProps('max_age_seconds')}
            />
            <Group justify="flex-end">
              <Button variant="default" onClick={() => setCorsModalOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" loading={corsMutation.isPending}>
                Save
              </Button>
            </Group>
          </Stack>
        </form>
      </Modal>
    </div>
  );
}
