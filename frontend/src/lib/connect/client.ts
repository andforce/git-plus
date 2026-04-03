import { createClient } from '@connectrpc/connect';
import { apiTransport } from './transport';
import { ConfigService } from '~rpc/gitplus/config/v1/config_pb';
import { EventService } from '~rpc/gitplus/event/v1/event_pb';
import { TaskService } from '~rpc/gitplus/task/v1/task_pb';

export const configClient = createClient(ConfigService, apiTransport);
export const taskClient = createClient(TaskService, apiTransport);
export const eventClient = createClient(EventService, apiTransport);
