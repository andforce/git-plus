import { createClient } from '@connectrpc/connect';
import { apiTransport } from './transport';
import { ConfigService } from '~rpc/gitplus/config/v1/config_pb';

export const configClient = createClient(ConfigService, apiTransport);
