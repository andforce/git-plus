import { Outlet, createFileRoute } from '@tanstack/react-router';

export const Route = createFileRoute('/_dashboard/config')({
  component: () => <Outlet />,
});
