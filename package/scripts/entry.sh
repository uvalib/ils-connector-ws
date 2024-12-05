#
#
#

# run from here
cd bin; ./ils-connector-ws \
  -authsecret ${AUTH_SHARED_SECRET} \
  -jwtkey ${JWT_KEY} \
  -jwtsecret "UNKNOWN" \
  -pda ${PDA_BASE_URL} \
  -port ${SERVICE_PORT} \
  -secretbase "UNKNOWN" \
  -sirsiclient ${SIRSI_CLIENT_ID} \
  -sirsilibrary ${SIRSI_LIBRARY} \
  -sirsipass ${SIRSI_PASSWORD} \
  -sirsiscript ${SIRSI_SCRIPT_URL} \
  -sirsiurl ${SIRSI_WEB_SERVICES_BASE} \
  -sirsiuser ${SIRSI_USER} \
  -userinfo ${USERINFO_URL} \
  -virgo ${SEARCH_URL}

return $?

#
# end of file
#
