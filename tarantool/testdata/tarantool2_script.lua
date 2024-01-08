#! /usr/bin/env tarantool
todo = todo or {}

function todo.info()
    return "info"
end

box.cfg {
    listen = 3301;
}

box.once('init/access', function ()
    box.schema.user.grant('guest', 'read,write,execute', 'universe')
end)