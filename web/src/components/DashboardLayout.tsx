import { useState } from 'react';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import {
  AppShell,
  Burger,
  Group,
  NavLink,
  Title,
  Avatar,
  Menu,
  Text,
  Divider,
  useMantineColorScheme,
  ActionIcon,
  Badge,
} from '@mantine/core';
import {
  IconDashboard,
  IconBucket,
  IconUsers,
  IconKey,
  IconServer,
  IconSettings,
  IconLogout,
  IconSun,
  IconMoon,
  IconCloud,
  IconShieldCheck,
  IconClipboardList,
  IconBrain,
  IconLock,
  IconDatabase,
  IconChartDots,
  IconApi,
} from '@tabler/icons-react';
import { useAuthStore } from '../stores/auth';

interface NavItem {
  label: string;
  icon: React.ReactNode;
  path: string;
  adminOnly?: boolean;
}

const navItems: NavItem[] = [
  { label: 'Dashboard', icon: <IconDashboard size={20} />, path: '/dashboard', adminOnly: true },
  { label: 'Buckets', icon: <IconBucket size={20} />, path: '/buckets' },
  { label: 'Users', icon: <IconUsers size={20} />, path: '/users', adminOnly: true },
  { label: 'Policies', icon: <IconShieldCheck size={20} />, path: '/policies', adminOnly: true },
  { label: 'Tiering Policies', icon: <IconDatabase size={20} />, path: '/tiering-policies', adminOnly: true },
  { label: 'Predictive Analytics', icon: <IconChartDots size={20} />, path: '/predictive-analytics', adminOnly: true },
  { label: 'Access Keys', icon: <IconKey size={20} />, path: '/access-keys' },
  { label: 'Cluster', icon: <IconServer size={20} />, path: '/cluster', adminOnly: true },
  { label: 'Audit Logs', icon: <IconClipboardList size={20} />, path: '/audit-logs', adminOnly: true },
  { label: 'AI/ML Features', icon: <IconBrain size={20} />, path: '/ai-ml-features', adminOnly: true },
  { label: 'Security', icon: <IconLock size={20} />, path: '/security', adminOnly: true },
  { label: 'API Explorer', icon: <IconApi size={20} />, path: '/api-explorer', adminOnly: true },
  { label: 'Settings', icon: <IconSettings size={20} />, path: '/settings' },
];

export function DashboardLayout() {
  const [opened, setOpened] = useState(false);
  const navigate = useNavigate();
  const location = useLocation();
  const { user, logout } = useAuthStore();
  const { colorScheme, toggleColorScheme } = useMantineColorScheme();

  const isAdmin = user?.role === 'superadmin' || user?.role === 'admin';

  const handleLogout = () => {
    logout();
    navigate('/login');
  };

  const filteredNavItems = navItems.filter((item) => !item.adminOnly || isAdmin);

  return (
    <AppShell
      header={{ height: 60 }}
      navbar={{ width: 250, breakpoint: 'sm', collapsed: { mobile: !opened } }}
      padding="md"
    >
      <AppShell.Header>
        <Group h="100%" px="md" justify="space-between">
          <Group>
            <Burger opened={opened} onClick={() => setOpened(!opened)} hiddenFrom="sm" size="sm" />
            <IconCloud size={32} color="var(--mantine-color-blue-6)" />
            <Title order={3}>NebulaIO</Title>
          </Group>

          <Group>
            <ActionIcon
              variant="default"
              onClick={() => toggleColorScheme()}
              size="lg"
              aria-label="Toggle color scheme"
            >
              {colorScheme === 'dark' ? <IconSun size={18} /> : <IconMoon size={18} />}
            </ActionIcon>

            <Menu shadow="md" width={200}>
              <Menu.Target>
                <Avatar
                  color="blue"
                  radius="xl"
                  style={{ cursor: 'pointer' }}
                >
                  {user?.username?.charAt(0).toUpperCase()}
                </Avatar>
              </Menu.Target>

              <Menu.Dropdown>
                <Menu.Label>
                  <Text size="sm" fw={500}>
                    {user?.username}
                  </Text>
                  <Badge size="xs" variant="light" mt={4}>
                    {user?.role}
                  </Badge>
                </Menu.Label>
                <Divider />
                <Menu.Item
                  leftSection={<IconSettings size={14} />}
                  onClick={() => navigate('/settings')}
                >
                  Settings
                </Menu.Item>
                <Menu.Item
                  color="red"
                  leftSection={<IconLogout size={14} />}
                  onClick={handleLogout}
                >
                  Logout
                </Menu.Item>
              </Menu.Dropdown>
            </Menu>
          </Group>
        </Group>
      </AppShell.Header>

      <AppShell.Navbar p="md">
        {filteredNavItems.map((item) => (
          <NavLink
            key={item.path}
            label={item.label}
            leftSection={item.icon}
            active={location.pathname.startsWith(item.path)}
            onClick={() => {
              navigate(item.path);
              setOpened(false);
            }}
            style={{ borderRadius: 'var(--mantine-radius-md)' }}
            mb={4}
          />
        ))}
      </AppShell.Navbar>

      <AppShell.Main>
        <Outlet />
      </AppShell.Main>
    </AppShell>
  );
}
