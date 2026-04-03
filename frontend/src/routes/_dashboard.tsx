import {
  Outlet,
  createFileRoute,
  useLocation,
  useNavigate,
} from '@tanstack/react-router';
import {
  AppShell,
  Burger,
  Group,
  NavLink,
  ScrollArea,
  Text,
} from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { IconBrandGithub, IconSettings2, IconTool } from '@tabler/icons-react';

export const Route = createFileRoute('/_dashboard')({
  component: DashboardLayout,
});

const childNavStyles = {
  root: { borderRadius: 'var(--mantine-radius-sm)' },
};

function DashboardLayout() {
  const [opened, { toggle, close }] = useDisclosure();
  const location = useLocation();
  const navigate = useNavigate();

  const navTo = (to: string) => {
    navigate({ to });
    close();
  };

  return (
    <AppShell
      navbar={{
        width: 240,
        breakpoint: 'sm',
        collapsed: { mobile: !opened },
      }}
      header={{ height: { base: 48, sm: 0 } }}
      padding="0"
    >
      <AppShell.Header hiddenFrom="sm">
        <Group h="100%" px="md">
          <Burger opened={opened} onClick={toggle} size="sm" />
          <Group gap={8}>
            <IconBrandGithub size={18} />
            <Text fw={600} size="sm">
              Git Plus
            </Text>
          </Group>
        </Group>
      </AppShell.Header>

      <AppShell.Navbar>
        <AppShell.Section p="md" pb="sm">
          <Group gap={8}>
            <IconBrandGithub size={22} />
            <Text fw={700} size="md">
              Git Plus
            </Text>
          </Group>
        </AppShell.Section>

        <AppShell.Section grow component={ScrollArea} px="xs" pt="xs">
          <NavLink
            label="Configuration"
            leftSection={<IconSettings2 size={16} />}
            defaultOpened
            styles={childNavStyles}
          >
            <NavLink
              label="Overview"
              active={location.pathname === '/config'}
              onClick={() => navTo('/config')}
              styles={childNavStyles}
            />
            <NavLink
              label="Sources"
              active={location.pathname === '/config/sources'}
              onClick={() => navTo('/config/sources')}
              styles={childNavStyles}
            />
            <NavLink
              label="Cron"
              active={location.pathname === '/config/cron'}
              onClick={() => navTo('/config/cron')}
              styles={childNavStyles}
            />
          </NavLink>
          <NavLink
            label="Maintenance"
            leftSection={<IconTool size={16} />}
            defaultOpened
            styles={childNavStyles}
          >
            <NavLink
              label="Tasks"
              active={location.pathname === '/maintenance/tasks'}
              onClick={() => navTo('/maintenance/tasks')}
              styles={childNavStyles}
            />
          </NavLink>
        </AppShell.Section>
      </AppShell.Navbar>

      <AppShell.Main>
        <Outlet />
      </AppShell.Main>
    </AppShell>
  );
}
