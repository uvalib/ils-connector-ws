#
#
#

# run from here
cd bin; ./ils-connector-ws \
   -api $V4_JMRL_API \
   -apikey $V4_JMRL_API_KEY \
   -apisecret $V4_JMRL_API_SECRET \
   -jwtkey $V4_JWT_KEY

return $?

#
# end of file
#
