import { LineChart } from '@mantine/charts';
import { Box, Card, Text, Title, alpha, useMantineTheme } from '@mantine/core';
import { ChartTooltip } from './chart-tooltip';

const metricData = [
  {
    date: 'Jan',
    average: 63,
    today: 75,
  },
  {
    date: 'Feb',
    average: 72,
    today: 85,
  },
  {
    date: 'Mar',
    average: 81,
    today: 95,
  },
  {
    date: 'Apr',
    average: 77,
    today: 88,
  },
  {
    date: 'May',
    average: 68,
    today: 78,
  },
  {
    date: 'Jun',
    average: 65,
    today: 82,
  },
  {
    date: 'Jul',
    average: 70,
    today: 90,
  },
  {
    date: 'Aug',
    average: 75,
    today: 85,
  },
  {
    date: 'Sep',
    average: 78,
    today: 88,
  },
  {
    date: 'Oct',
    average: 73,
    today: 83,
  },
  {
    date: 'Nov',
    average: 68,
    today: 78,
  },
  {
    date: 'Dec',
    average: 62,
    today: 72,
  },
];

export function CardsMetric() {
  const theme = useMantineTheme();

  return (
    <Card>
      <Box pb="md">
        <Title order={5}>Exercise Minutes</Title>
        <Text size="sm" c="dimmed">
          Your exercise minutes are ahead of where you normally are.
        </Text>
      </Box>
      <Box>
        <div style={{ height: 200 }}>
          <LineChart
            h={200}
            data={metricData}
            dataKey="date"
            series={[
              {
                name: 'average',
                color: alpha(`var(--mantine-primary-color-filled)`, 0.25),
              },
              { name: 'today', color: theme.primaryColor },
            ]}
            tickLine="none"
            gridAxis="none"
            withXAxis={false}
            withYAxis={false}
            tooltipProps={{
              content: ({ label, payload }) => (
                <ChartTooltip label={String(label)} payload={payload} />
              ),
            }}
            dotProps={{ strokeWidth: 2, fill: '#fff' }}
            lineProps={(series) => ({
              dataKey: series.name,
              isAnimationActive: true,
            })}
          />
        </div>
      </Box>
    </Card>
  );
}
