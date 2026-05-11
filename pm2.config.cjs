module.exports = {
  apps: [
    {
      name: 'git-plus',
      script: 'dist/server/index.cjs',
      args: '--data-dir ./data',
      interpreter: 'node',
      env: {
        NODE_ENV: 'production',
        PORT: '8080',
      },
      max_memory_restart: '512M',
      time: true,
    },
  ],
};
