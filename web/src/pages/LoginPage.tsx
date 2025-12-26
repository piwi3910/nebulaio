import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Container,
  Paper,
  Title,
  TextInput,
  PasswordInput,
  Button,
  Group,
  Text,
  Center,
  Stack,
} from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import { IconCloud } from '@tabler/icons-react';
import { adminApi } from '../api/client';
import { useAuthStore } from '../stores/auth';

interface LoginFormValues {
  username: string;
  password: string;
}

export function LoginPage() {
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();
  const { setTokens, setUser } = useAuthStore();

  const form = useForm<LoginFormValues>({
    initialValues: {
      username: '',
      password: '',
    },
    validate: {
      username: (value) => (value.length < 1 ? 'Username is required' : null),
      password: (value) => (value.length < 1 ? 'Password is required' : null),
    },
  });

  const handleSubmit = async (values: LoginFormValues) => {
    setLoading(true);
    try {
      const response = await adminApi.login(values.username, values.password);
      const { access_token, refresh_token } = response.data;

      setTokens(access_token, refresh_token);

      // Decode JWT to get user info (basic parsing)
      const payload = JSON.parse(atob(access_token.split('.')[1]));
      setUser({
        id: payload.user_id,
        username: payload.username,
        role: payload.role,
      });

      notifications.show({
        title: 'Welcome!',
        message: `Logged in as ${values.username}`,
        color: 'green',
      });

      navigate('/');
    } catch {
      notifications.show({
        title: 'Login failed',
        message: 'Invalid username or password',
        color: 'red',
      });
    } finally {
      setLoading(false);
    }
  };

  return (
    <Container size={420} my={40}>
      <Center mb="xl">
        <Group>
          <IconCloud size={48} color="var(--mantine-color-blue-6)" />
          <Title order={1}>NebulaIO</Title>
        </Group>
      </Center>

      <Paper withBorder shadow="md" p={30} radius="md">
        <Title order={2} ta="center" mb="md">
          Sign in
        </Title>
        <Text c="dimmed" size="sm" ta="center" mb="lg">
          Enter your credentials to access the console
        </Text>

        <form onSubmit={form.onSubmit(handleSubmit)}>
          <Stack>
            <TextInput
              label="Username"
              placeholder="admin"
              required
              {...form.getInputProps('username')}
            />
            <PasswordInput
              label="Password"
              placeholder="Your password"
              required
              {...form.getInputProps('password')}
            />
            <Button type="submit" fullWidth mt="md" loading={loading}>
              Sign in
            </Button>
          </Stack>
        </form>
      </Paper>
    </Container>
  );
}
