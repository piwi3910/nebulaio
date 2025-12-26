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
  Stack,
  Badge,
  Skeleton,
  Paper,
  CopyButton,
  Tooltip,
  Alert,
  Code,
} from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import { IconPlus, IconTrash, IconCopy, IconCheck, IconAlertCircle } from '@tabler/icons-react';
import { consoleApi } from '../api/client';
import { useAuthStore } from '../stores/auth';

interface AccessKey {
  access_key_id: string;
  secret_access_key?: string;
  description: string;
  enabled: boolean;
  created_at: string;
}

export function AccessKeysPage() {
  const [createModalOpen, setCreateModalOpen] = useState(false);
  const [newKey, setNewKey] = useState<AccessKey | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const queryClient = useQueryClient();
  const { user } = useAuthStore();

  const { data: keys, isLoading } = useQuery({
    queryKey: ['my-access-keys'],
    queryFn: () => consoleApi.listMyAccessKeys().then((res) => res.data),
  });

  const createMutation = useMutation({
    mutationFn: (description: string) => consoleApi.createMyAccessKey(description),
    onSuccess: (response) => {
      queryClient.invalidateQueries({ queryKey: ['my-access-keys'] });
      setCreateModalOpen(false);
      form.reset();
      setNewKey(response.data);
      notifications.show({
        title: 'Access key created',
        message: 'Make sure to save your secret key - it will only be shown once!',
        color: 'green',
      });
    },
    onError: () => {
      notifications.show({
        title: 'Error',
        message: 'Failed to create access key',
        color: 'red',
      });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (accessKeyId: string) => consoleApi.deleteMyAccessKey(accessKeyId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['my-access-keys'] });
      setDeleteTarget(null);
      notifications.show({
        title: 'Access key deleted',
        message: 'The access key has been deleted successfully',
        color: 'green',
      });
    },
    onError: () => {
      notifications.show({
        title: 'Error',
        message: 'Failed to delete access key',
        color: 'red',
      });
    },
  });

  const form = useForm({
    initialValues: {
      description: '',
    },
  });

  return (
    <div>
      <Group justify="space-between" mb="lg">
        <div>
          <Title order={2}>Access Keys</Title>
          <Text c="dimmed" size="sm">
            Manage your S3 API access keys for {user?.username}
          </Text>
        </div>
        <Button leftSection={<IconPlus size={16} />} onClick={() => setCreateModalOpen(true)}>
          Create Access Key
        </Button>
      </Group>

      <Paper withBorder>
        {isLoading ? (
          <Stack p="md">
            <Skeleton height={40} />
            <Skeleton height={40} />
          </Stack>
        ) : keys?.length ? (
          <Table striped highlightOnHover>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Access Key ID</Table.Th>
                <Table.Th>Description</Table.Th>
                <Table.Th>Status</Table.Th>
                <Table.Th>Created</Table.Th>
                <Table.Th style={{ width: 80 }}>Actions</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {keys.map((key: AccessKey) => (
                <Table.Tr key={key.access_key_id}>
                  <Table.Td>
                    <Group gap="xs">
                      <Code>{key.access_key_id}</Code>
                      <CopyButton value={key.access_key_id} timeout={2000}>
                        {({ copied, copy }) => (
                          <Tooltip label={copied ? 'Copied' : 'Copy'}>
                            <ActionIcon
                              color={copied ? 'teal' : 'gray'}
                              variant="subtle"
                              onClick={copy}
                            >
                              {copied ? <IconCheck size={14} /> : <IconCopy size={14} />}
                            </ActionIcon>
                          </Tooltip>
                        )}
                      </CopyButton>
                    </Group>
                  </Table.Td>
                  <Table.Td>{key.description || '-'}</Table.Td>
                  <Table.Td>
                    <Badge color={key.enabled ? 'green' : 'red'} variant="light">
                      {key.enabled ? 'Active' : 'Disabled'}
                    </Badge>
                  </Table.Td>
                  <Table.Td>{new Date(key.created_at).toLocaleDateString()}</Table.Td>
                  <Table.Td>
                    <ActionIcon
                      color="red"
                      variant="subtle"
                      onClick={() => setDeleteTarget(key.access_key_id)}
                    >
                      <IconTrash size={16} />
                    </ActionIcon>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        ) : (
          <Text c="dimmed" ta="center" p="xl">
            No access keys found. Create one to start using the S3 API.
          </Text>
        )}
      </Paper>

      {/* Create Access Key Modal */}
      <Modal
        opened={createModalOpen}
        onClose={() => setCreateModalOpen(false)}
        title="Create Access Key"
      >
        <form onSubmit={form.onSubmit((values) => createMutation.mutate(values.description))}>
          <Stack>
            <TextInput
              label="Description"
              placeholder="My application key"
              {...form.getInputProps('description')}
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

      {/* New Key Display Modal */}
      <Modal
        opened={newKey !== null}
        onClose={() => setNewKey(null)}
        title="Access Key Created"
        size="lg"
      >
        <Alert icon={<IconAlertCircle size={16} />} color="yellow" mb="md">
          <Text fw={500}>Save your secret key now!</Text>
          <Text size="sm">
            This is the only time you will be able to see the secret access key. Make sure to copy
            and store it securely.
          </Text>
        </Alert>

        <Stack>
          <div>
            <Text size="sm" fw={500} mb={4}>
              Access Key ID
            </Text>
            <Group gap="xs">
              <Code style={{ flex: 1 }}>{newKey?.access_key_id}</Code>
              <CopyButton value={newKey?.access_key_id || ''} timeout={2000}>
                {({ copied, copy }) => (
                  <Button
                    size="xs"
                    color={copied ? 'teal' : 'blue'}
                    variant="light"
                    onClick={copy}
                    leftSection={copied ? <IconCheck size={14} /> : <IconCopy size={14} />}
                  >
                    {copied ? 'Copied' : 'Copy'}
                  </Button>
                )}
              </CopyButton>
            </Group>
          </div>

          <div>
            <Text size="sm" fw={500} mb={4}>
              Secret Access Key
            </Text>
            <Group gap="xs">
              <Code style={{ flex: 1 }}>{newKey?.secret_access_key}</Code>
              <CopyButton value={newKey?.secret_access_key || ''} timeout={2000}>
                {({ copied, copy }) => (
                  <Button
                    size="xs"
                    color={copied ? 'teal' : 'blue'}
                    variant="light"
                    onClick={copy}
                    leftSection={copied ? <IconCheck size={14} /> : <IconCopy size={14} />}
                  >
                    {copied ? 'Copied' : 'Copy'}
                  </Button>
                )}
              </CopyButton>
            </Group>
          </div>

          <Button fullWidth onClick={() => setNewKey(null)} mt="md">
            I have saved my credentials
          </Button>
        </Stack>
      </Modal>

      {/* Delete Confirmation Modal */}
      <Modal
        opened={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title="Delete Access Key"
      >
        <Text mb="md">
          Are you sure you want to delete access key <strong>{deleteTarget}</strong>? Applications
          using this key will no longer be able to authenticate.
        </Text>
        <Group justify="flex-end">
          <Button variant="default" onClick={() => setDeleteTarget(null)}>
            Cancel
          </Button>
          <Button
            color="red"
            onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)}
            loading={deleteMutation.isPending}
          >
            Delete
          </Button>
        </Group>
      </Modal>
    </div>
  );
}
