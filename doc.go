/*

GOP (GOlang-in-Production) is a collection of code intended to ease writing production-safe golang
applications, particularly web applications.

Providing configuration

GOP expects you to create an ini-formatted configuration file for your application. It will
attempt to load it during the call gop.Init(), and will raise an error if it cannot find it.

Here is how GOP determines the location of the configuration file:

 * If you set the environment variable called $PROJECT_$APP_CFG_FILE (all uppercase), where
   $PROJECT and $APP are the projectName and appName parameters that you have passed to
   gop.Init() as arguments, GOP will check that location for the configuration file

 * If you set the environment variable called $PROJECT_CFG_ROOT (all uppercase), GOP will
   check that directory for the file named $APP.conf

 * If you did not set any of these environment variables, GOP will look for a file named
   $APP.conf in /etc/$PROJECT

It should be emphasized that GOP will check only one location. It means that if you specified
$PROJECT_$APP_CFG_FILE and the file does not exist, GOP will raise an error.

To summarize it:

  pathToConfigFile = $PROJECT_$APP_CFG_FILE || $PROJECT_CFG_ROOT/$APP.conf || /etc/$PROJECT/$APP.conf

Overriding configuration

There are certain cases, when you may want to override parts of your configuration. GOP provides
a mechanism for doing that. Simply create a JSON file next to the config file that GOP will use.
The file should have the same name as that config file, but also have the ".override" extension
appended to it. Example:

  Config:    /etc/my_gop_project/my_gop_app.conf
  Override:  /etc/my_gop_project/my_gop_app.conf.override

In fact, GOP will warn you if it does not find the overrides file. And an empty file will not
satisfy it - it has to be a valid JSON.

There is also a restriction to the contents of the overrides file:

  * The root element must be an associative array (can be empty)
  * The keys of the root element must be strings (section names)
  * The values of the root element must be associative arrays (section options)
  * The keys and values of the associative arrays that are the values of the root element must be quoted

To illustrate these requirements:

  []                              # Bad. Root element is not an associative array.
  {"version": "2"}                # Bad. Values of the root element must be associative arrays.
  {"overrides": {"version": 2}}   # Bad. Version is not quoted.
  {"overrides": {"version": "2"}} # Good.
  {}                              # Good. Minimum viable config.

Accessing configuration

You can access the application's configuration via the Cfg property of the app instance returned
by gop.Init(). This property has type Config.

Logging

GOP uses Timber (https://github.com/jbert/timber) for logging. A *gop.App instance embeds the
interface of timber.Logger, which means all of its methods can be accessed like this:

  app := gop.Init("myproject", "myapp")
  app.Debug("My debug message")

Configuring Logging

The logger is configured during the call to gop.Init(). The following options are available
in the [gop] section of the configuration file (values shown below are default):

  log_pattern         = "[%D %T] [%L] %S"                 # Log message format accepted by Timber
  log_filename        = false                             # Show file path and line number of the method that created log message.
                                                          #   This option may not work with custom log pattern (include %S to avoid it).

  log_dir             = /var/log                          # Directory where GOP will look for the project's log directory
  log_file            = $log_dir/$project_name/$app.log   # Full path to the log file
  log_level           = INFO                              # Case-insensitive log level accepted by Timber: Finest, Fine, Debug, Trace, Info, Warn, Error, Critical
  stdout_only_logging = false                             # Output log to STDOUT instead of the log file

If the path to the log_file does not exist and stdout_only_logging is false, GOP will raise an error.

GOP HTTP Handlers

GOP provides a few HTTP handlers, all beginning with "/gop", that you can enable by setting enable_gop_urls to
true in the [gop] section of your configuration file. Otherwise, GOP will respond with "not enabled" when you
will try to access those handlers.

The following handlers are available:

  /gop/config/:section/:key

    When the HTTP verb is PUT, GOP will override the config setting specified by :section and :key (the value
    should be specified in the body of the request).

    An example command line to change a config value using curl is:

    echo -n info | curl -s -T - http://127.0.0.1:1732/gop/config/gop/log_level

    When the HTTP verb is GET, you can read a specific key value. You can also omit :key or both :key and :section to return sections or the entire config.

 /gop/status

    TODO

 /gop/stack

    TODO

 /gop/mem

    TODO

 /gop/test?secs=int&kbytes=int

    TODO

*/
package gop
