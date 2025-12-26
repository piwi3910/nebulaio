import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
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
  Menu,
} from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import {
  IconPlus,
  IconTrash,
  IconFolder,
  IconDotsVertical,
  IconSettings,
} from '@tabler/icons-react';
import { adminApi, consoleApi } from '../api/client';
import { useAuthStore } from '../stores/auth';

interface Bucket {
  name: string;
  owner: string;
  created_at: string;
  region: string;
  storage_class: string;
  versioning: string;
}

export function BucketsPage() {
  const [createModalOpen, setCreateModalOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { user } = useAuthStore();
  const isAdmin = user?.role === 'superadmin' || user?.role === 'admin';

  const { data: buckets, isLoading } = useQuery({
    queryKey: ['buckets'],
    queryFn: () =>
      isAdmin
        ? adminApi.listBuckets().then((res) => res.data)
        : consoleApi.listMyBuckets().then((res) => res.data),
  });

  const createMutation = useMutation({
    mutationFn: (data: { name: string; region?: string }) => adminApi.createBucket(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['buckets'] });
      setCreateModalOpen(false);
      form.reset();
      notifications.show({
        title: 'Bucket created',
        message: 'The bucket has been created successfully',
        color: 'green',
      });
    },
    onError: (error: Error & { response?: { data?: { error?: string } } }) => {
      notifications.show({
        title: 'Error',
        message: error.response?.data?.error || 'Failed to create bucket',
        color: 'red',
      });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (name: string) => adminApi.deleteBucket(name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['buckets'] });
      setDeleteTarget(null);
      notifications.show({
        title: 'Bucket deleted',
        message: 'The bucket has been deleted successfully',
        color: 'green',
      });
    },
    onError: (error: Error & { response?: { data?: { error?: string } } }) => {
      notifications.show({
        title: 'Error',
        message: error.response?.data?.error || 'Failed to delete bucket',
        color: 'red',
      });
    },
  });

  const form = useForm({
    initialValues: {
      name: '',
      region: 'us-east-1',
    },
    validate: {
      name: (value) => {
        if (value.length < 3) return 'Bucket name must be at least 3 characters';
        if (value.length > 63) return 'Bucket name must be at most 63 characters';
        if (!/^[a-z0-9][a-z0-9.-]*[a-z0-9]$/.test(value)) {
          return 'Bucket name must start and end with a letter or number';
        }
        return null;
      },
    },
  });

  const handleCreate = (values: { name: string; region: string }) => {
    createMutation.mutate(values);
  };

  const handleBrowse = (bucketName: string) => {
    navigate(`/buckets/${bucketName}`);
  };

  return (
    <div>
      <Group justify="space-between" mb="lg">
        <Title order={2}>Buckets</Title>
        {isAdmin && (
          <Button leftSection={<IconPlus size={16} />} onClick={() => setCreateModalOpen(true)}>
            Create Bucket
          </Button>
        )}
      </Group>

      <Paper withBorder>
        {isLoading ? (
          <Stack p="md">
            <Skeleton height={40} />
            <Skeleton height={40} />
            <Skeleton height={40} />
          </Stack>
        ) : buckets?.length ? (
          <Table striped highlightOnHover>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Name</Table.Th>
                <Table.Th>Region</Table.Th>
                <Table.Th>Storage Class</Table.Th>
                <Table.Th>Versioning</Table.Th>
                <Table.Th>Created</Table.Th>
                <Table.Th style={{ width: 80 }}>Actions</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {buckets.map((bucket: Bucket) => (
                <Table.Tr
                  key={bucket.name}
                  style={{ cursor: 'pointer' }}
                  onClick={() => handleBrowse(bucket.name)}
                >
                  <Table.Td>
                    <Group gap="xs">
                      <IconFolder size={16} />
                      <Text fw={500}>{bucket.name}</Text>
                    </Group>
                  </Table.Td>
                  <Table.Td>{bucket.region || 'us-east-1'}</Table.Td>
                  <Table.Td>
                    <Badge variant="light" size="sm">
                      {bucket.storage_class || 'STANDARD'}
                    </Badge>
                  </Table.Td>
                  <Table.Td>
                    <Badge
                      variant="light"
                      color={bucket.versioning === 'Enabled' ? 'green' : 'gray'}
                      size="sm"
                    >
                      {bucket.versioning || 'Disabled'}
                    </Badge>
                  </Table.Td>
                  <Table.Td>{new Date(bucket.created_at).toLocaleDateString()}</Table.Td>
                  <Table.Td onClick={(e) => e.stopPropagation()}>
                    <Menu shadow="md" width={150}>
                      <Menu.Target>
                        <ActionIcon variant="subtle">
                          <IconDotsVertical size={16} />
                        </ActionIcon>
                      </Menu.Target>
                      <Menu.Dropdown>
                        <Menu.Item
                          leftSection={<IconFolder size={14} />}
                          onClick={() => handleBrowse(bucket.name)}
                        >
                          Browse
                        </Menu.Item>
                        {isAdmin && (
                          <>
                            <Menu.Item leftSection={<IconSettings size={14} />}>Settings</Menu.Item>
                            <Menu.Divider />
                            <Menu.Item
                              color="red"
                              leftSection={<IconTrash size={14} />}
                              onClick={() => setDeleteTarget(bucket.name)}
                            >
                              Delete
                            </Menu.Item>
                          </>
                        )}
                      </Menu.Dropdown>
                    </Menu>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        ) : (
          <Text c="dimmed" ta="center" p="xl">
            No buckets found. Create your first bucket to get started.
          </Text>
        )}
      </Paper>

      {/* Create Bucket Modal */}
      <Modal
        opened={createModalOpen}
        onClose={() => setCreateModalOpen(false)}
        title="Create Bucket"
      >
        <form onSubmit={form.onSubmit(handleCreate)}>
          <Stack>
            <TextInput
              label="Bucket Name"
              placeholder="my-bucket"
              required
              {...form.getInputProps('name')}
            />
            <TextInput
              label="Region"
              placeholder="us-east-1"
              {...form.getInputProps('region')}
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

      {/* Delete Confirmation Modal */}
      <Modal
        opened={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title="Delete Bucket"
      >
        <Text mb="md">
          Are you sure you want to delete bucket <strong>{deleteTarget}</strong>? This action cannot
          be undone.
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
