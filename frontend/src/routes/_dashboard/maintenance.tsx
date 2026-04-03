import { Outlet, createFileRoute } from '@tanstack/react-router';

export const Route = createFileRoute('/_dashboard/maintenance')({
  component: () => <Outlet />,
});
