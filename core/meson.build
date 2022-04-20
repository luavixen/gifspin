project('gifspin-core', 'c', default_options: ['c_std=c99'])

cc = meson.get_compiler('c')

dep_libm = cc.find_library('m', required: false)
dep_libvips = dependency('vips')

executable('gifspin-core', 'spin.c', dependencies: [dep_libm, dep_libvips])
