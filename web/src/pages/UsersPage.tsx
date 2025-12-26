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
  PasswordInput,
  Select,
  Stack,
  Badge,
  Skeleton,
  Paper,
  Menu,
  Switch,
} from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import { IconPlus, IconTrash, IconDotsVertical, IconEdit, IconKey } from '@tabler/icons-react';
import { adminApi } from '../api/client';

interface User {
  id: string;
  username: string;
  email: string;
  role: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export function UsersPage() {
  const [createModalOpen, setCreateModalOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<User | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<User | null>(null);
  const queryClient = useQueryClient();

  const { data: users, isLoading } = useQuery({
    queryKey: ['users'],
    queryFn: () => adminApi.listUsers().then((res) => res.data),
  });

  const createMutation = useMutation({
    mutationFn: (data: { username: string; password: string; email: string; role: string }) =>
      adminApi.createUser(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
      setCreateModalOpen(false);
      createForm.reset();
      notifications.show({
        title: 'User created',
        message: 'The user has been created successfully',
        color: 'green',
      });
    },
    onError: (error: Error & { response?: { data?: { error?: string } } }) => {
      notifications.show({
        title: 'Error',
        message: error.response?.data?.error || 'Failed to create user',
        color: 'red',
      });
    },
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<User> }) =>
      adminApi.updateUser(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
      setEditTarget(null);
      notifications.show({
        title: 'User updated',
        message: 'The user has been updated successfully',
        color: 'green',
      });
    },
    onError: () => {
      notifications.show({
        title: 'Error',
        message: 'Failed to update user',
        color: 'red',
      });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => adminApi.deleteUser(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
      setDeleteTarget(null);
      notifications.show({
        title: 'User deleted',
        message: 'The user has been deleted successfully',
        color: 'green',
      });
    },
    onError: () => {
      notifications.show({
        title: 'Error',
        message: 'Failed to delete user',
        color: 'red',
      });
    },
  });

  const createForm = useForm({
    initialValues: {
      username: '',
      password: '',
      email: '',
      role: 'user',
    },
    validate: {
      username: (value) => (value.length < 3 ? 'Username must be at least 3 characters' : null),
      password: (value) => (value.length < 8 ? 'Password must be at least 8 characters' : null),
      email: (value) => (/^\S+@\S+$/.test(value) || value === '' ? null : 'Invalid email'),
    },
  });

  const editForm = useForm({
    initialValues: {
      email: '',
      role: 'user',
      enabled: true,
    },
  });

  const handleEdit = (user: User) => {
    editForm.setValues({
      email: user.email,
      role: user.role,
      enabled: user.enabled,
    });
    setEditTarget(user);
  };

  const roleColors: Record<string, string> = {
    superadmin: 'red',
    admin: 'orange',
    user: 'blue',
    readonly: 'gray',
    service: 'violet',
  };

  return (
    <div>
      <Group justify="space-between" mb="lg">
        <Title order={2}>Users</Title>
        <Button leftSection={<IconPlus size={16} />} onClick={() => setCreateModalOpen(true)}>
          Create User
        </Button>
      </Group>

      <Paper withBorder>
        {isLoading ? (
          <Stack p="md">
            <Skeleton height={40} />
            <Skeleton height={40} />
            <Skeleton height={40} />
          </Stack>
        ) : users?.length ? (
          <Table striped highlightOnHover>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Username</Table.Th>
                <Table.Th>Email</Table.Th>
                <Table.Th>Role</Table.Th>
                <Table.Th>Status</Table.Th>
                <Table.Th>Created</Table.Th>
                <Table.Th style={{ width: 80 }}>Actions</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {users.map((user: User) => (
                <Table.Tr key={user.id}>
                  <Table.Td>
                    <Text fw={500}>{user.username}</Text>
                  </Table.Td>
                  <Table.Td>{user.email || '-'}</Table.Td>
                  <Table.Td>
                    <Badge color={roleColors[user.role] || 'gray'} variant="light">
                      {user.role}
                    </Badge>
                  </Table.Td>
                  <Table.Td>
                    <Badge color={user.enabled ? 'green' : 'red'} variant="light">
                      {user.enabled ? 'Active' : 'Disabled'}
                    </Badge>
                  </Table.Td>
                  <Table.Td>{new Date(user.created_at).toLocaleDateString()}</Table.Td>
                  <Table.Td>
                    <Menu shadow="md" width={150}>
                      <Menu.Target>
                        <ActionIcon variant="subtle">
                          <IconDotsVertical size={16} />
                        </ActionIcon>
                      </Menu.Target>
                      <Menu.Dropdown>
                        <Menu.Item
                          leftSection={<IconEdit size={14} />}
                          onClick={() => handleEdit(user)}
                        >
                          Edit
                        </Menu.Item>
                        <Menu.Item leftSection={<IconKey size={14} />}>Access Keys</Menu.Item>
                        <Menu.Divider />
                        <Menu.Item
                          color="red"
                          leftSection={<IconTrash size={14} />}
                          onClick={() => setDeleteTarget(user)}
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
            No users found
          </Text>
        )}
      </Paper>

      {/* Create User Modal */}
      <Modal
        opened={createModalOpen}
        onClose={() => setCreateModalOpen(false)}
        title="Create User"
      >
        <form onSubmit={createForm.onSubmit((values) => createMutation.mutate(values))}>
          <Stack>
            <TextInput
              label="Username"
              placeholder="johndoe"
              required
              {...createForm.getInputProps('username')}
            />
            <PasswordInput
              label="Password"
              placeholder="********"
              required
              {...createForm.getInputProps('password')}
            />
            <TextInput
              label="Email"
              placeholder="john@example.com"
              {...createForm.getInputProps('email')}
            />
            <Select
              label="Role"
              data={[
                { value: 'user', label: 'User' },
                { value: 'admin', label: 'Admin' },
                { value: 'readonly', label: 'Read Only' },
                { value: 'service', label: 'Service Account' },
              ]}
              {...createForm.getInputProps('role')}
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

      {/* Edit User Modal */}
      <Modal
        opened={editTarget !== null}
        onClose={() => setEditTarget(null)}
        title={`Edit User: ${editTarget?.username}`}
      >
        <form
          onSubmit={editForm.onSubmit((values) =>
            updateMutation.mutate({ id: editTarget!.id, data: values })
          )}
        >
          <Stack>
            <TextInput
              label="Email"
              placeholder="john@example.com"
              {...editForm.getInputProps('email')}
            />
            <Select
              label="Role"
              data={[
                { value: 'user', label: 'User' },
                { value: 'admin', label: 'Admin' },
                { value: 'readonly', label: 'Read Only' },
                { value: 'service', label: 'Service Account' },
              ]}
              {...editForm.getInputProps('role')}
            />
            <Switch
              label="Enabled"
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

      {/* Delete Confirmation Modal */}
      <Modal
        opened={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title="Delete User"
      >
        <Text mb="md">
          Are you sure you want to delete user <strong>{deleteTarget?.username}</strong>? This
          action cannot be undone.
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
