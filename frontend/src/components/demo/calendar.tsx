import { useState } from 'react';
import { Card } from '@mantine/core';
import { DatePicker } from '@mantine/dates';
import type { DatesRangeValue } from '@mantine/dates';

export function CardsCalendar() {
  const [value, setValue] = useState<DatesRangeValue>([
    new Date(2023, 5, 5),
    new Date(2023, 5, 13),
  ]);

  return (
    <Card p="md">
      <DatePicker
        size="sm"
        type="range"
        value={value}
        onChange={(dates) => {
          setValue(dates);
        }}
        firstDayOfWeek={0}
        defaultDate={new Date(2023, 5)}
        withCellSpacing={false}
      />
    </Card>
  );
}
