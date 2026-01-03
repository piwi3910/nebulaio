module.exports = {
  plugins: {
    'postcss-preset-mantine': {},
    'postcss-simple-vars': {
      variables: {
        'mantine-breakpoint-xs': '36em',
        'mantine-breakpoint-sm': '48em',
        'mantine-breakpoint-md': '62em',
        'mantine-breakpoint-lg': '75em',
        'mantine-breakpoint-xl': '88em',
      },
      // Ignore unknown variables (e.g., from swagger-ui CSS which uses SCSS variables)
      // Return the original $variable reference to leave it unchanged
      unknown: function (node, name) {
        return '$' + name;
      },
    },
  },
};
