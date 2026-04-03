import { Link, createFileRoute } from '@tanstack/react-router';
import {
  Badge,
  Box,
  Button,
  Container,
  Divider,
  Group,
  Stack,
  Text,
  Title,
} from '@mantine/core';
import {
  IconAlertTriangle,
  IconArrowRight,
  IconCircleCheck,
  IconExclamationCircle,
} from '@tabler/icons-react';
import { useSuspenseQuery } from '@tanstack/react-query';
import {
  configCheckQueryOptions,
  configQueryOptions,
} from '~lib/config-queries';
import { ValidationIssue_Severity } from '~rpc/gitplus/config/v1/config_pb';

export const Route = createFileRoute('/_dashboard/config/')({
  loader: async ({ context: { queryClient } }) => {
    await Promise.all([
      queryClient.ensureQueryData(configQueryOptions),
      queryClient.ensureQueryData(configCheckQueryOptions),
    ]);
  },
  component: ConfigOverview,
});

function ConfigOverview() {
  const { data: configData } = useSuspenseQuery(configQueryOptions);
  const { data: checkData } = useSuspenseQuery(configCheckQueryOptions);

  const config = configData.config;
  const sourceCount = config?.sources.length ?? 0;
  const concurrency = config?.concurrency ?? 0;
  const errorCount = checkData.summary?.error ?? 0;
  const warningCount = checkData.summary?.warning ?? 0;
  const hasIssues = checkData.issues.length > 0;

  return (
    <Container fluid py="xl" px="xl">
      <Title order={2}>Configuration</Title>
      <Text c="dimmed" size="sm" mb="xl">
        Overview of your Git Plus setup
      </Text>

      <Stack gap="lg">
        <Box>
          <Text size="xs" fw={500} c="dimmed" tt="uppercase" mb={4}>
            Health
          </Text>
          {errorCount > 0 ? (
            <Group gap={4}>
              <IconExclamationCircle
                size={14}
                style={{ color: 'var(--mantine-color-red-6)' }}
              />
              <Text fw={600} c="red">
                {errorCount} error{errorCount !== 1 && 's'}
              </Text>
            </Group>
          ) : warningCount > 0 ? (
            <Group gap={4}>
              <IconAlertTriangle
                size={14}
                style={{ color: 'var(--mantine-color-orange-6)' }}
              />
              <Text fw={600} c="orange">
                {warningCount} warning{warningCount !== 1 && 's'}
              </Text>
            </Group>
          ) : (
            <Group gap={4}>
              <IconCircleCheck
                size={14}
                style={{ color: 'var(--mantine-color-teal-6)' }}
              />
              <Text fw={600} c="teal">
                Healthy
              </Text>
            </Group>
          )}
        </Box>

        {hasIssues && (
          <>
            <Divider />
            <Box>
              <Text size="xs" fw={500} c="dimmed" tt="uppercase" mb="sm">
                Validation issues
              </Text>
              <Stack gap="xs">
                {checkData.issues.map((issue, i) => (
                  <Group key={i} gap="xs" wrap="nowrap">
                    <Badge
                      size="xs"
                      variant="light"
                      color={severityColor(issue.severity)}
                      miw={56}
                    >
                      {severityLabel(issue.severity)}
                    </Badge>
                    <Text
                      size="sm"
                      c={severityColor(issue.severity)}
                      style={{ flex: 1 }}
                    >
                      {issue.message}
                    </Text>
                  </Group>
                ))}
              </Stack>
            </Box>
          </>
        )}

        <Divider />

        <Stack gap="xs" maw={280}>
          <Group justify="space-between">
            <Text size="sm" c="dimmed">
              Sources
            </Text>
            <Text size="sm" fw={600}>
              {sourceCount} configured
            </Text>
          </Group>
          <Group justify="space-between">
            <Text size="sm" c="dimmed">
              Concurrency
            </Text>
            <Text size="sm" fw={600}>
              {concurrency} parallel
            </Text>
          </Group>
        </Stack>

        <Divider />

        <Group>
          <Button
            component={Link}
            to="/config/sources"
            variant="light"
            rightSection={<IconArrowRight size={16} />}
          >
            Manage Sources
          </Button>
        </Group>
      </Stack>
    </Container>
  );
}

function severityColor(severity: ValidationIssue_Severity) {
  switch (severity) {
    case ValidationIssue_Severity.ERROR:
      return 'red';
    case ValidationIssue_Severity.WARNING:
      return 'orange';
    case ValidationIssue_Severity.INFO:
      return 'blue';
    default:
      return 'gray';
  }
}

function severityLabel(severity: ValidationIssue_Severity) {
  switch (severity) {
    case ValidationIssue_Severity.ERROR:
      return 'Error';
    case ValidationIssue_Severity.WARNING:
      return 'Warning';
    case ValidationIssue_Severity.INFO:
      return 'Info';
    default:
      return 'Unknown';
  }
}
