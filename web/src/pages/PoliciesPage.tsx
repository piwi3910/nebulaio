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
  Tabs,
  Code,
  ScrollArea,
  Tooltip,
} from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import {
  IconPlus,
  IconTrash,
  IconDotsVertical,
  IconEdit,
  IconEye,
  IconCopy,
  IconShieldCheck,
} from '@tabler/icons-react';
import { adminApi, Policy } from '../api/client';
import { PolicyEditor } from '../components/PolicyEditor';

export function PoliciesPage() {
  const [createModalOpen, setCreateModalOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<Policy | null>(null);
  const [viewTarget, setViewTarget] = useState<Policy | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Policy | null>(null);
  const queryClient = useQueryClient();

  const { data: policies, isLoading } = useQuery({
    queryKey: ['policies'],
    queryFn: () => adminApi.listPolicies().then((res) => res.data as Policy[]),
  });

  const createMutation = useMutation({
    mutationFn: (data: { name: string; description: string; document: string }) =>
      adminApi.createPolicy(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['policies'] });
      setCreateModalOpen(false);
      createForm.reset();
      notifications.show({
        title: 'Policy created',
        message: 'The policy has been created successfully',
        color: 'green',
      });
    },
    onError: (error: Error & { response?: { data?: { error?: string } } }) => {
      notifications.show({
        title: 'Error',
        message: error.response?.data?.error || 'Failed to create policy',
        color: 'red',
      });
    },
  });

  const updateMutation = useMutation({
    mutationFn: ({ name, data }: { name: string; data: { description: string; document: string } }) =>
      adminApi.updatePolicy(name, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['policies'] });
      setEditTarget(null);
      notifications.show({
        title: 'Policy updated',
        message: 'The policy has been updated successfully',
        color: 'green',
      });
    },
    onError: (error: Error & { response?: { data?: { error?: string } } }) => {
      notifications.show({
        title: 'Error',
        message: error.response?.data?.error || 'Failed to update policy',
        color: 'red',
      });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (name: string) => adminApi.deletePolicy(name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['policies'] });
      setDeleteTarget(null);
      notifications.show({
        title: 'Policy deleted',
        message: 'The policy has been deleted successfully',
        color: 'green',
      });
    },
    onError: (error: Error & { response?: { data?: { error?: string } } }) => {
      notifications.show({
        title: 'Error',
        message: error.response?.data?.error || 'Failed to delete policy',
        color: 'red',
      });
    },
  });

  const createForm = useForm({
    initialValues: {
      name: '',
      description: '',
      document: JSON.stringify(
        {
          Version: '2012-10-17',
          Statement: [
            {
              Effect: 'Allow',
              Action: ['s3:GetObject'],
              Resource: ['arn:aws:s3:::*/*'],
            },
          ],
        },
        null,
        2
      ),
    },
    validate: {
      name: (value) => {
        if (value.length < 1) return 'Name is required';
        if (!/^[a-zA-Z0-9_-]+$/.test(value)) {
          return 'Name can only contain letters, numbers, hyphens, and underscores';
        }
        return null;
      },
      document: (value) => {
        try {
          const doc = JSON.parse(value);
          if (!doc.Version) return 'Policy must have a Version field';
          if (!doc.Statement) return 'Policy must have a Statement field';
          return null;
        } catch {
          return 'Invalid JSON';
        }
      },
    },
  });

  const editForm = useForm({
    initialValues: {
      description: '',
      document: '',
    },
    validate: {
      document: (value) => {
        try {
          const doc = JSON.parse(value);
          if (!doc.Version) return 'Policy must have a Version field';
          if (!doc.Statement) return 'Policy must have a Statement field';
          return null;
        } catch {
          return 'Invalid JSON';
        }
      },
    },
  });

  const handleEdit = (policy: Policy) => {
    let formattedDoc = policy.document;
    try {
      formattedDoc = JSON.stringify(JSON.parse(policy.document), null, 2);
    } catch {
      // Keep original if parsing fails
    }
    editForm.setValues({
      description: policy.description,
      document: formattedDoc,
    });
    setEditTarget(policy);
  };

  const handleView = (policy: Policy) => {
    setViewTarget(policy);
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    notifications.show({
      title: 'Copied',
      message: 'Policy document copied to clipboard',
      color: 'blue',
    });
  };

  const getStatementSummary = (document: string): string => {
    try {
      const doc = JSON.parse(document);
      const statements = doc.Statement?.length || 0;
      return `${statements} statement${statements !== 1 ? 's' : ''}`;
    } catch {
      return 'Invalid';
    }
  };

  const getEffects = (document: string): { allow: number; deny: number } => {
    try {
      const doc = JSON.parse(document);
      const statements = doc.Statement || [];
      return {
        allow: statements.filter((s: { Effect: string }) => s.Effect === 'Allow').length,
        deny: statements.filter((s: { Effect: string }) => s.Effect === 'Deny').length,
      };
    } catch {
      return { allow: 0, deny: 0 };
    }
  };

  return (
    <div>
      <Group justify="space-between" mb="lg">
        <Group>
          <IconShieldCheck size={28} color="var(--mantine-color-blue-6)" />
          <Title order={2}>IAM Policies</Title>
        </Group>
        <Button leftSection={<IconPlus size={16} />} onClick={() => setCreateModalOpen(true)}>
          Create Policy
        </Button>
      </Group>

      <Paper withBorder>
        {isLoading ? (
          <Stack p="md">
            <Skeleton height={40} />
            <Skeleton height={40} />
            <Skeleton height={40} />
          </Stack>
        ) : policies?.length ? (
          <Table striped highlightOnHover>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Name</Table.Th>
                <Table.Th>Description</Table.Th>
                <Table.Th>Statements</Table.Th>
                <Table.Th>Effects</Table.Th>
                <Table.Th>Updated</Table.Th>
                <Table.Th style={{ width: 80 }}>Actions</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {policies.map((policy: Policy) => {
                const effects = getEffects(policy.document);
                return (
                  <Table.Tr key={policy.name}>
                    <Table.Td>
                      <Text fw={500}>{policy.name}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" c="dimmed" lineClamp={1}>
                        {policy.description || '-'}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Badge variant="light" size="sm">
                        {getStatementSummary(policy.document)}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <Group gap={4}>
                        {effects.allow > 0 && (
                          <Badge color="green" variant="light" size="sm">
                            {effects.allow} Allow
                          </Badge>
                        )}
                        {effects.deny > 0 && (
                          <Badge color="red" variant="light" size="sm">
                            {effects.deny} Deny
                          </Badge>
                        )}
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" c="dimmed">
                        {new Date(policy.updated_at).toLocaleDateString()}
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
                            leftSection={<IconEdit size={14} />}
                            onClick={() => handleEdit(policy)}
                          >
                            Edit
                          </Menu.Item>
                          <Menu.Item
                            leftSection={<IconCopy size={14} />}
                            onClick={() => copyToClipboard(policy.document)}
                          >
                            Copy JSON
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
                );
              })}
            </Table.Tbody>
          </Table>
        ) : (
          <Text c="dimmed" ta="center" p="xl">
            No policies found. Create your first policy to get started.
          </Text>
        )}
      </Paper>

      {/* Create Policy Modal */}
      <Modal
        opened={createModalOpen}
        onClose={() => setCreateModalOpen(false)}
        title="Create Policy"
        size="xl"
      >
        <form onSubmit={createForm.onSubmit((values) => createMutation.mutate(values))}>
          <Stack>
            <TextInput
              label="Policy Name"
              placeholder="my-read-only-policy"
              required
              description="Alphanumeric characters, hyphens, and underscores only"
              {...createForm.getInputProps('name')}
            />
            <Textarea
              label="Description"
              placeholder="A policy that allows read-only access..."
              minRows={2}
              {...createForm.getInputProps('description')}
            />
            <div>
              <Text size="sm" fw={500} mb="xs">
                Policy Document
              </Text>
              <PolicyEditor
                value={createForm.values.document}
                onChange={(value) => createForm.setFieldValue('document', value)}
                error={createForm.errors.document as string}
              />
            </div>
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
            updateMutation.mutate({ name: editTarget!.name, data: values })
          )}
        >
          <Stack>
            <Textarea
              label="Description"
              placeholder="A policy that allows read-only access..."
              minRows={2}
              {...editForm.getInputProps('description')}
            />
            <div>
              <Text size="sm" fw={500} mb="xs">
                Policy Document
              </Text>
              <PolicyEditor
                value={editForm.values.document}
                onChange={(value) => editForm.setFieldValue('document', value)}
                error={editForm.errors.document as string}
              />
            </div>
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
          <Tabs defaultValue="document">
            <Tabs.List>
              <Tabs.Tab value="document">Document</Tabs.Tab>
              <Tabs.Tab value="details">Details</Tabs.Tab>
            </Tabs.List>

            <Tabs.Panel value="document" pt="md">
              <Group justify="flex-end" mb="xs">
                <Tooltip label="Copy to clipboard">
                  <ActionIcon
                    variant="subtle"
                    onClick={() => copyToClipboard(viewTarget.document)}
                  >
                    <IconCopy size={16} />
                  </ActionIcon>
                </Tooltip>
              </Group>
              <ScrollArea h={400}>
                <Code block style={{ whiteSpace: 'pre-wrap' }}>
                  {(() => {
                    try {
                      return JSON.stringify(JSON.parse(viewTarget.document), null, 2);
                    } catch {
                      return viewTarget.document;
                    }
                  })()}
                </Code>
              </ScrollArea>
            </Tabs.Panel>

            <Tabs.Panel value="details" pt="md">
              <Stack>
                <Group>
                  <Text size="sm" c="dimmed" w={100}>
                    Name:
                  </Text>
                  <Text size="sm" fw={500}>
                    {viewTarget.name}
                  </Text>
                </Group>
                <Group>
                  <Text size="sm" c="dimmed" w={100}>
                    Description:
                  </Text>
                  <Text size="sm">{viewTarget.description || '-'}</Text>
                </Group>
                <Group>
                  <Text size="sm" c="dimmed" w={100}>
                    Created:
                  </Text>
                  <Text size="sm">{new Date(viewTarget.created_at).toLocaleString()}</Text>
                </Group>
                <Group>
                  <Text size="sm" c="dimmed" w={100}>
                    Updated:
                  </Text>
                  <Text size="sm">{new Date(viewTarget.updated_at).toLocaleString()}</Text>
                </Group>
              </Stack>
            </Tabs.Panel>
          </Tabs>
        )}
      </Modal>

      {/* Delete Confirmation Modal */}
      <Modal
        opened={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title="Delete Policy"
      >
        <Text mb="md">
          Are you sure you want to delete policy <strong>{deleteTarget?.name}</strong>? This action
          cannot be undone. Any users or groups attached to this policy will lose the associated
          permissions.
        </Text>
        <Group justify="flex-end">
          <Button variant="default" onClick={() => setDeleteTarget(null)}>
            Cancel
          </Button>
          <Button
            color="red"
            onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget.name)}
            loading={deleteMutation.isPending}
          >
            Delete
          </Button>
        </Group>
      </Modal>
    </div>
  );
}
