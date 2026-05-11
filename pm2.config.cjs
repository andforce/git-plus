module.exports = {
  apps: [
    {
      name: 'git-plus',
      script: 'dist/server/index.cjs',
      interpreter: 'node',
      env: {
        NODE_ENV: 'production',
        PORT: '8000',
      },
      max_memory_restart: '512M',
      time: true,
    },
  ],
};
