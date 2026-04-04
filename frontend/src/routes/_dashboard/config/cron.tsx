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

type ScheduleType =
  | 'daily'
  | 'hourly'
  | 'minutes'
  | 'weekdays'
  | 'weekly'
  | 'monthly';

const SCHEDULE_TYPES: Array<{ value: ScheduleType; label: string }> = [
  { value: 'daily', label: 'Every day' },
  { value: 'hourly', label: 'Hourly' },
  { value: 'minutes', label: 'Every N minutes' },
  { value: 'weekdays', label: 'Weekdays (Mon–Fri)' },
  { value: 'weekly', label: 'Weekly' },
  { value: 'monthly', label: 'Monthly' },
];

const HOUR_OPTIONS = Array.from({ length: 24 }, (_, i) => ({
  value: String(i),
  label: String(i).padStart(2, '0'),
}));

const MINUTE_OPTIONS = Array.from({ length: 12 }, (_, i) => ({
  value: String(i * 5),
  label: String(i * 5).padStart(2, '0'),
}));

const HOUR_INTERVAL_OPTIONS = [1, 2, 3, 4, 6, 8, 12].map((n) => ({
  value: String(n),
  label: n === 1 ? 'Every hour' : `Every ${n} hours`,
}));

const MINUTE_INTERVAL_OPTIONS = [1, 2, 5, 10, 15, 20, 30, 45].map((n) => ({
  value: String(n),
  label: n === 1 ? 'Every minute' : `Every ${n} minutes`,
}));

const WEEKDAY_OPTIONS = [
  { value: '1', label: 'Monday' },
  { value: '2', label: 'Tuesday' },
  { value: '3', label: 'Wednesday' },
  { value: '4', label: 'Thursday' },
  { value: '5', label: 'Friday' },
  { value: '6', label: 'Saturday' },
  { value: '0', label: 'Sunday' },
];

function buildExpression(
  type: ScheduleType,
  hour: number,
  minute: number,
  hourInterval: number,
  minuteInterval: number,
  weekday: string,
  monthDay: number,
): string {
  switch (type) {
    case 'daily':
      return `${minute} ${hour} * * *`;
    case 'hourly':
      return hourInterval === 1
        ? `${minute} * * * *`
        : `${minute} */${hourInterval} * * *`;
    case 'minutes':
      return minuteInterval === 1 ? '* * * * *' : `*/${minuteInterval} * * * *`;
    case 'weekdays':
      return `${minute} ${hour} * * 1-5`;
    case 'weekly':
      return `${minute} ${hour} * * ${weekday}`;
    case 'monthly':
      return `${minute} ${hour} ${monthDay} * *`;
  }
}

function describeSchedule(
  type: ScheduleType,
  hour: number,
  minute: number,
  hourInterval: number,
  minuteInterval: number,
  weekday: string,
  monthDay: number,
): string {
  const time = `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`;
  switch (type) {
    case 'daily':
      return `Runs every day at ${time}`;
    case 'hourly':
      return hourInterval === 1
        ? `Runs every hour at :${String(minute).padStart(2, '0')}`
        : `Runs every ${hourInterval} hours at :${String(minute).padStart(2, '0')}`;
    case 'minutes':
      return minuteInterval === 1
        ? 'Runs every minute'
        : `Runs every ${minuteInterval} minutes`;
    case 'weekdays':
      return `Runs Monday through Friday at ${time}`;
    case 'weekly': {
      const dayName =
        WEEKDAY_OPTIONS.find((d) => d.value === weekday)?.label ?? '';
      return `Runs every ${dayName} at ${time}`;
    }
    case 'monthly':
      return `Runs on day ${monthDay} of every month at ${time}`;
  }
}

function TimePicker({
  hour,
  minute,
  onHourChange,
  onMinuteChange,
}: {
  hour: number;
  minute: number;
  onHourChange: (h: number) => void;
  onMinuteChange: (m: number) => void;
}) {
  return (
    <Group gap={4} align="center" wrap="nowrap">
      <Text size="sm" c="dimmed">
        at
      </Text>
      <Select
        data={HOUR_OPTIONS}
        value={String(hour)}
        onChange={(v) => v && onHourChange(Number(v))}
        allowDeselect={false}
        w={72}
        size="sm"
        comboboxProps={{ width: 80 }}
      />
      <Text fw={600} size="lg">
        :
      </Text>
      <Select
        data={MINUTE_OPTIONS}
        value={String(minute)}
        onChange={(v) => v && onMinuteChange(Number(v))}
        allowDeselect={false}
        w={72}
        size="sm"
        comboboxProps={{ width: 80 }}
      />
    </Group>
  );
}

function CronBuilder({ onApply }: { onApply: (expr: string) => void }) {
  const [opened, { toggle, close }] = useDisclosure(false);
  const [scheduleType, setScheduleType] = useState<ScheduleType>('daily');
  const [hour, setHour] = useState(9);
  const [minute, setMinute] = useState(0);
  const [hourInterval, setHourInterval] = useState(1);
  const [minuteInterval, setMinuteInterval] = useState(30);
  const [weekday, setWeekday] = useState('1');
  const [monthDay, setMonthDay] = useState(1);

  const generatedExpr = useMemo(
    () =>
      buildExpression(
        scheduleType,
        hour,
        minute,
        hourInterval,
        minuteInterval,
        weekday,
        monthDay,
      ),
    [
      scheduleType,
      hour,
      minute,
      hourInterval,
      minuteInterval,
      weekday,
      monthDay,
    ],
  );

  const description = useMemo(
    () =>
      describeSchedule(
        scheduleType,
        hour,
        minute,
        hourInterval,
        minuteInterval,
        weekday,
        monthDay,
      ),
    [
      scheduleType,
      hour,
      minute,
      hourInterval,
      minuteInterval,
      weekday,
      monthDay,
    ],
  );

  const renderInputs = () => {
    switch (scheduleType) {
      case 'daily':
      case 'weekdays':
        return (
          <TimePicker
            hour={hour}
            minute={minute}
            onHourChange={setHour}
            onMinuteChange={setMinute}
          />
        );
      case 'hourly':
        return (
          <Group gap="sm" align="center" wrap="nowrap">
            <Select
              data={HOUR_INTERVAL_OPTIONS}
              value={String(hourInterval)}
              onChange={(v) => v && setHourInterval(Number(v))}
              allowDeselect={false}
              style={{ flex: 1 }}
              size="sm"
            />
            <Text size="sm" c="dimmed">
              at minute
            </Text>
            <Select
              data={MINUTE_OPTIONS}
              value={String(minute)}
              onChange={(v) => v && setMinute(Number(v))}
              allowDeselect={false}
              w={72}
              size="sm"
              comboboxProps={{ width: 80 }}
            />
          </Group>
        );
      case 'minutes':
        return (
          <Select
            data={MINUTE_INTERVAL_OPTIONS}
            value={String(minuteInterval)}
            onChange={(v) => v && setMinuteInterval(Number(v))}
            allowDeselect={false}
            size="sm"
          />
        );
      case 'weekly':
        return (
          <Stack gap="sm">
            <Group gap="sm" align="center" wrap="nowrap">
              <Text size="sm" c="dimmed">
                on
              </Text>
              <Select
                data={WEEKDAY_OPTIONS}
                value={weekday}
                onChange={(v) => v && setWeekday(v)}
                allowDeselect={false}
                style={{ flex: 1 }}
                size="sm"
              />
            </Group>
            <TimePicker
              hour={hour}
              minute={minute}
              onHourChange={setHour}
              onMinuteChange={setMinute}
            />
          </Stack>
        );
      case 'monthly':
        return (
          <Stack gap="sm">
            <Group gap="sm" align="center" wrap="nowrap">
              <Text size="sm" c="dimmed">
                on day
              </Text>
              <NumberInput
                value={monthDay}
                onChange={(v) => setMonthDay(typeof v === 'number' ? v : 1)}
                min={1}
                max={31}
                w={72}
                size="sm"
              />
            </Group>
            <TimePicker
              hour={hour}
              minute={minute}
              onHourChange={setHour}
              onMinuteChange={setMinute}
            />
          </Stack>
        );
      default:
        return null;
    }
  };

  return (
    <Popover
      opened={opened}
      onClose={close}
      position="bottom-start"
      shadow="md"
      width={340}
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
          <Select
            label="Schedule type"
            data={SCHEDULE_TYPES}
            value={scheduleType}
            onChange={(v) => v && setScheduleType(v)}
            allowDeselect={false}
            size="sm"
          />

          {renderInputs()}

          <Box>
            <Code>{generatedExpr}</Code>
            <Text size="xs" c="dimmed" mt={4}>
              {description}
            </Text>
          </Box>

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
