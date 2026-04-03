import { useMemo, useState } from 'react';
import { createFileRoute } from '@tanstack/react-router';
import {
  Badge,
  Box,
  Button,
  Code,
  Container,
  Divider,
  Group,
  NumberInput,
  Popover,
  Select,
  Stack,
  Text,
  TextInput,
  Title,
} from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import {
  IconCalendarEvent,
  IconClock,
  IconDeviceFloppy,
} from '@tabler/icons-react';
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from '@tanstack/react-query';
import { ConnectError } from '@connectrpc/connect';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import { parseCronExpression } from 'cron-schedule';
import { toast } from 'sonner';
import dayjs from 'dayjs';
import { cronClient } from '~lib/connect/client';
import { cronRuntimeQueryOptions } from '~lib/cron-queries';

export const Route = createFileRoute('/_dashboard/config/cron')({
  loader: ({ context: { queryClient } }) =>
    queryClient.ensureQueryData(cronRuntimeQueryOptions),
  component: CronPage,
});

function getErrorMessage(error: unknown): string {
  if (error instanceof ConnectError) return error.message;
  return 'An unexpected error occurred';
}

function formatTimestamp(ts: Parameters<typeof timestampDate>[0] | undefined) {
  if (!ts) return 'N/A';
  return dayjs(timestampDate(ts)).format('YYYY-MM-DD HH:mm:ss');
}

function formatRelative(ts: Parameters<typeof timestampDate>[0] | undefined) {
  if (!ts) return '';
  return dayjs(timestampDate(ts)).fromNow();
}

function previewNextDates(expr: string, count: number): Array<Date> | string {
  const parts = expr.trim().split(/\s+/);
  if (parts.length !== 5) {
    return 'Expression must have exactly 5 fields (minute hour day month weekday)';
  }
  try {
    const cron = parseCronExpression(expr);
    return cron.getNextDates(count);
  } catch (e) {
    return e instanceof Error ? e.message : 'Invalid cron expression';
  }
}

const CRON_UNITS = [
  { value: 'minute', label: 'Minute(s)', min: 1, max: 59 },
  { value: 'hour', label: 'Hour(s)', min: 1, max: 23 },
  { value: 'day', label: 'Day of the month', min: 1, max: 31 },
  { value: 'month', label: 'Month(s)', min: 1, max: 12 },
  { value: 'weekday', label: 'Day of the week', min: 0, max: 6 },
];

function stepOrWild(value: number): string {
  return value === 1 ? '*' : `*/${value}`;
}

function buildCronExpression(unit: string, value: number): string {
  switch (unit) {
    case 'minute':
      return `${stepOrWild(value)} * * * *`;
    case 'hour':
      return `0 ${stepOrWild(value)} * * *`;
    case 'day':
      return `0 0 ${stepOrWild(value)} * *`;
    case 'month':
      return `0 0 1 ${stepOrWild(value)} *`;
    case 'weekday':
      return `0 0 * * ${value}`;
    default:
      return '* * * * *';
  }
}

function CronBuilder({ onApply }: { onApply: (expr: string) => void }) {
  const [opened, { toggle, close }] = useDisclosure(false);
  const [unit, setUnit] = useState<string>('day');
  const [value, setValue] = useState<number>(1);

  const currentUnit = CRON_UNITS.find((u) => u.value === unit) ?? CRON_UNITS[0];
  const clampedValue = Math.min(
    Math.max(value, currentUnit.min),
    currentUnit.max,
  );
  const generatedExpr = buildCronExpression(unit, clampedValue);

  return (
    <Popover
      opened={opened}
      onClose={close}
      position="bottom-start"
      shadow="md"
      width={320}
    >
      <Popover.Target>
        <Button
          variant="default"
          size="sm"
          leftSection={<IconClock size={16} />}
          onClick={toggle}
        >
          Cron Builder
        </Button>
      </Popover.Target>
      <Popover.Dropdown>
        <Stack gap="sm">
          <Text size="sm" fw={500}>
            Execute schedule every
          </Text>
          <Group gap="sm" wrap="nowrap">
            <NumberInput
              value={value}
              onChange={(v) => setValue(typeof v === 'number' ? v : value)}
              min={currentUnit.min}
              max={currentUnit.max}
              style={{ width: 80 }}
            />
            <Select
              data={CRON_UNITS.map((u) => ({
                value: u.value,
                label: u.label,
              }))}
              value={unit}
              onChange={(v) => {
                if (!v) return;
                setUnit(v);
                const newUnit = CRON_UNITS.find((u) => u.value === v);
                if (newUnit) setValue(newUnit.min);
              }}
              allowDeselect={false}
              style={{ flex: 1 }}
            />
          </Group>
          <Text size="xs" c="dimmed">
            Valid range: {currentUnit.min}–{currentUnit.max}
          </Text>
          <Group gap="xs" align="center">
            <Code>{generatedExpr}</Code>
          </Group>
          <Button
            fullWidth
            onClick={() => {
              onApply(generatedExpr);
              close();
            }}
          >
            Apply
          </Button>
        </Stack>
      </Popover.Dropdown>
    </Popover>
  );
}

function CronPage() {
  const { data } = useSuspenseQuery(cronRuntimeQueryOptions);
  const queryClient = useQueryClient();
  const runtime = data.runtime;

  const [expression, setExpression] = useState('');
  const isEditing = expression.trim().length > 0;

  const preview = useMemo(() => {
    const trimmed = expression.trim();
    if (!trimmed) return null;
    return previewNextDates(trimmed, 5);
  }, [expression]);

  const previewError = typeof preview === 'string' ? preview : null;
  const previewDates = Array.isArray(preview) ? preview : null;

  const updateMutation = useMutation({
    mutationFn: (cron: string) => cronClient.updateCron({ cron }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['cron', 'runtime'] });
      setExpression('');
      toast.success('Cron schedule updated');
    },
    onError: (error) => toast.error(getErrorMessage(error)),
  });

  const disableMutation = useMutation({
    mutationFn: () => cronClient.updateCron({ cron: '' }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['cron', 'runtime'] });
      setExpression('');
      toast.success('Cron schedule disabled');
    },
    onError: (error) => toast.error(getErrorMessage(error)),
  });

  const handleSave = () => {
    const trimmed = expression.trim();
    if (!trimmed || previewError) return;
    updateMutation.mutate(trimmed);
  };

  return (
    <Container fluid py="xl" px="xl">
      <Title order={2}>Cron Schedule</Title>
      <Text c="dimmed" size="sm" mb="xl">
        Configure automatic sync schedule
      </Text>

      <Stack gap="lg">
        {/* Current schedule */}
        <Box>
          <Text size="xs" fw={500} c="dimmed" tt="uppercase" mb="sm">
            Current schedule
          </Text>
          <Stack gap="xs" maw={360}>
            <Group justify="space-between">
              <Text size="sm" c="dimmed">
                Status
              </Text>
              <Badge
                size="sm"
                variant="light"
                color={runtime?.enabled ? 'teal' : 'gray'}
              >
                {runtime?.enabled ? 'Enabled' : 'Disabled'}
              </Badge>
            </Group>
            <Group justify="space-between">
              <Text size="sm" c="dimmed">
                Expression
              </Text>
              <Text size="sm" fw={600}>
                {runtime?.cron || 'Not set'}
              </Text>
            </Group>
            {runtime?.lastError && (
              <Group justify="space-between" align="flex-start">
                <Text size="sm" c="dimmed">
                  Error
                </Text>
                <Text size="sm" c="red" ta="right" maw={220}>
                  {runtime.lastError}
                </Text>
              </Group>
            )}
          </Stack>
        </Box>

        {/* Next scheduled runs from API */}
        {runtime?.enabled && runtime.nextRuns.length > 0 && (
          <>
            <Divider />
            <Box>
              <Text size="xs" fw={500} c="dimmed" tt="uppercase" mb="sm">
                Upcoming runs
              </Text>
              <Stack gap={4}>
                {runtime.nextRuns.map((ts, i) => (
                  <Group key={i} gap="sm">
                    <IconCalendarEvent
                      size={14}
                      style={{ color: 'var(--mantine-color-dimmed)' }}
                    />
                    <Text size="sm" ff="monospace">
                      {formatTimestamp(ts)}
                    </Text>
                    <Text size="xs" c="dimmed">
                      {formatRelative(ts)}
                    </Text>
                  </Group>
                ))}
              </Stack>
            </Box>
          </>
        )}

        <Divider />

        {/* Edit section */}
        <Box>
          <Text size="xs" fw={500} c="dimmed" tt="uppercase" mb="sm">
            Update schedule
          </Text>
          <Stack gap="md" maw={480}>
            <div>
              <Group gap="sm" align="flex-end" mb={4}>
                <TextInput
                  placeholder="e.g. 0 */6 * * *"
                  label="Cron expression"
                  value={expression}
                  onChange={(e) => setExpression(e.currentTarget.value)}
                  error={isEditing ? previewError : undefined}
                  style={{ flex: 1 }}
                />
                <CronBuilder onApply={setExpression} />
              </Group>
              {!previewError && (
                <Text size="xs" c="dimmed">
                  Standard 5-field format: minute hour day month weekday
                </Text>
              )}
            </div>

            {/* Client-side preview */}
            {previewDates && (
              <Box>
                <Text size="xs" fw={500} c="dimmed" mb="xs">
                  Preview for <Code>{expression.trim()}</Code>
                </Text>
                <Stack gap={4}>
                  {previewDates.map((date, i) => (
                    <Group key={i} gap="sm">
                      <IconCalendarEvent
                        size={14}
                        style={{ color: 'var(--mantine-color-blue-5)' }}
                      />
                      <Text size="sm" ff="monospace">
                        {dayjs(date).format('YYYY-MM-DD HH:mm:ss')}
                      </Text>
                      <Text size="xs" c="dimmed">
                        {dayjs(date).fromNow()}
                      </Text>
                    </Group>
                  ))}
                </Stack>
              </Box>
            )}

            <Group gap="sm">
              <Button
                leftSection={<IconDeviceFloppy size={16} />}
                onClick={handleSave}
                loading={updateMutation.isPending}
                disabled={!isEditing || !!previewError}
              >
                Save
              </Button>
              {runtime?.enabled && (
                <Button
                  variant="default"
                  onClick={() => disableMutation.mutate()}
                  loading={disableMutation.isPending}
                >
                  Disable
                </Button>
              )}
            </Group>
          </Stack>
        </Box>
      </Stack>
    </Container>
  );
}
