import { useState } from 'react';
import {
  Title,
  Card,
  Text,
  Stack,
  PasswordInput,
  Button,
  Group,
  Divider,
  Badge,
  TextInput,
} from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import { useMutation } from '@tanstack/react-query';
import { IconUser, IconKey, IconShield } from '@tabler/icons-react';
import { useAuthStore } from '../stores/auth';
import { consoleApi } from '../api/client';

export function SettingsPage() {
  const { user, logout } = useAuthStore();
  const [showPasswordForm, setShowPasswordForm] = useState(false);

  const passwordForm = useForm({
    initialValues: {
      currentPassword: '',
      newPassword: '',
      confirmPassword: '',
    },
    validate: {
      currentPassword: (value) => (value.length < 1 ? 'Current password is required' : null),
      newPassword: (value) =>
        value.length < 8 ? 'New password must be at least 8 characters' : null,
      confirmPassword: (value, values) =>
        value !== values.newPassword ? 'Passwords do not match' : null,
    },
  });

  const passwordMutation = useMutation({
    mutationFn: (values: { currentPassword: string; newPassword: string }) =>
      consoleApi.updatePassword(values.currentPassword, values.newPassword),
    onSuccess: () => {
      passwordForm.reset();
      setShowPasswordForm(false);
      notifications.show({
        title: 'Password updated',
        message: 'Your password has been changed successfully. Please log in again.',
        color: 'green',
      });
      // Log out user after password change
      setTimeout(() => logout(), 2000);
    },
    onError: () => {
      notifications.show({
        title: 'Error',
        message: 'Failed to update password. Please check your current password.',
        color: 'red',
      });
    },
  });

  const getRoleBadgeColor = (role: string): string => {
    switch (role) {
      case 'superadmin':
        return 'red';
      case 'admin':
        return 'orange';
      case 'user':
        return 'blue';
      case 'readonly':
        return 'gray';
      default:
        return 'blue';
    }
  };

  return (
    <div>
      <Title order={2} mb="lg">
        Settings
      </Title>

      <Stack>
        {/* Profile Section */}
        <Card withBorder radius="md" p="lg">
          <Group mb="md">
            <IconUser size={24} />
            <Text fw={500} size="lg">
              Profile
            </Text>
          </Group>

          <Stack gap="sm">
            <Group>
              <Text c="dimmed" w={120}>
                Username:
              </Text>
              <Text fw={500}>{user?.username}</Text>
            </Group>
            <Group>
              <Text c="dimmed" w={120}>
                User ID:
              </Text>
              <Text size="sm" c="dimmed">
                {user?.id}
              </Text>
            </Group>
            <Group>
              <Text c="dimmed" w={120}>
                Email:
              </Text>
              <Text>{user?.email || 'Not set'}</Text>
            </Group>
            <Group>
              <Text c="dimmed" w={120}>
                Role:
              </Text>
              <Badge color={getRoleBadgeColor(user?.role || '')} variant="light">
                {user?.role}
              </Badge>
            </Group>
          </Stack>
        </Card>

        {/* Security Section */}
        <Card withBorder radius="md" p="lg">
          <Group mb="md">
            <IconShield size={24} />
            <Text fw={500} size="lg">
              Security
            </Text>
          </Group>

          {!showPasswordForm ? (
            <Button
              variant="light"
              leftSection={<IconKey size={16} />}
              onClick={() => setShowPasswordForm(true)}
            >
              Change Password
            </Button>
          ) : (
            <form
              onSubmit={passwordForm.onSubmit((values) =>
                passwordMutation.mutate({
                  currentPassword: values.currentPassword,
                  newPassword: values.newPassword,
                })
              )}
            >
              <Stack>
                <PasswordInput
                  label="Current Password"
                  placeholder="Enter your current password"
                  required
                  {...passwordForm.getInputProps('currentPassword')}
                />
                <PasswordInput
                  label="New Password"
                  placeholder="Enter new password"
                  required
                  {...passwordForm.getInputProps('newPassword')}
                />
                <PasswordInput
                  label="Confirm New Password"
                  placeholder="Confirm new password"
                  required
                  {...passwordForm.getInputProps('confirmPassword')}
                />
                <Group>
                  <Button type="submit" loading={passwordMutation.isPending}>
                    Update Password
                  </Button>
                  <Button
                    variant="default"
                    onClick={() => {
                      setShowPasswordForm(false);
                      passwordForm.reset();
                    }}
                  >
                    Cancel
                  </Button>
                </Group>
              </Stack>
            </form>
          )}
        </Card>

        {/* S3 Configuration Section */}
        <Card withBorder radius="md" p="lg">
          <Group mb="md">
            <IconKey size={24} />
            <Text fw={500} size="lg">
              S3 API Configuration
            </Text>
          </Group>

          <Text size="sm" c="dimmed" mb="md">
            Use these settings to configure your S3 client or SDK.
          </Text>

          <Stack gap="sm">
            <TextInput
              label="Endpoint URL"
              value={`${window.location.protocol}//${window.location.hostname}:9000`}
              readOnly
              description="Use this URL as your S3 endpoint"
            />
            <TextInput
              label="Region"
              value="us-east-1"
              readOnly
              description="Default region for all buckets"
            />
          </Stack>

          <Divider my="md" />

          <Text size="sm" c="dimmed" mb="sm">
            Example AWS CLI configuration:
          </Text>
          <Card withBorder bg="dark.8" p="sm">
            <Text size="xs" ff="monospace" style={{ whiteSpace: 'pre-wrap' }}>
              {`aws configure set aws_access_key_id YOUR_ACCESS_KEY
aws configure set aws_secret_access_key YOUR_SECRET_KEY
aws configure set default.region us-east-1

# Use with NebulaIO
aws --endpoint-url http://${window.location.hostname}:9000 s3 ls`}
            </Text>
          </Card>
        </Card>
      </Stack>
    </div>
  );
}
