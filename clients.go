/* Client functions */

/*
 * Copyright (c) 2013-2014, Jeremy Bingham (<jbingham@gmail.com>)
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"encoding/json"
	"github.com/ctdk/goiardi/acl"
	"github.com/ctdk/goiardi/actor"
	"github.com/ctdk/goiardi/client"
	"github.com/ctdk/goiardi/loginfo"
	"github.com/ctdk/goiardi/organization"
	"github.com/ctdk/goiardi/util"
	"github.com/gorilla/mux"
	"net/http"
)

func clientHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	vars := mux.Vars(r)
	org, orgerr := organization.Get(vars["org"])
	if orgerr != nil {
		jsonErrorReport(w, r, orgerr.Error(), orgerr.Status())
		return
	}

	clientName := vars["name"]
	opUser, oerr := actor.GetReqUser(org, r.Header.Get("X-OPS-USERID"))
	if oerr != nil {
		jsonErrorReport(w, r, oerr.Error(), oerr.Status())
		return
	}

	switch r.Method {
	case "DELETE":
		chefClient, gerr := client.Get(org, clientName)
		if gerr != nil {
			jsonErrorReport(w, r, gerr.Error(), gerr.Status())
			return
		}
		clientACL, gerr := acl.GetItemACL(org, chefClient)
		if gerr != nil {
			jsonErrorReport(w, r, gerr.Error(), gerr.Status())
			return
		}
		if f, err := clientACL.CheckPerm("delete", opUser); err != nil {
			jsonErrorReport(w, r, err.Error(), err.Status())
			return
		} else if !f && !opUser.IsSelf(chefClient) {
			jsonErrorReport(w, r, "Deleting that client is forbidden", http.StatusForbidden)
			return
		}
		/* Docs were incorrect. It does want the body of the
		 * deleted object. */
		jsonClient := chefClient.ToJSON()

		/* Log the delete event before deleting the client, in
		 * case the client is deleting itself. */
		if lerr := loginfo.LogEvent(org, opUser, chefClient, "delete"); lerr != nil {
			jsonErrorReport(w, r, lerr.Error(), http.StatusInternalServerError)
			return
		}
		err := chefClient.Delete()
		if err != nil {
			jsonErrorReport(w, r, err.Error(), http.StatusForbidden)
			return
		}

		enc := json.NewEncoder(w)
		if err = enc.Encode(&jsonClient); err != nil {
			jsonErrorReport(w, r, err.Error(), http.StatusInternalServerError)
			return
		}
	case "GET":
		chefClient, gerr := client.Get(org, clientName)

		if gerr != nil {
			jsonErrorReport(w, r, gerr.Error(), gerr.Status())
			return
		}
		clientACL, gerr := acl.GetItemACL(org, chefClient)
		if gerr != nil {
			jsonErrorReport(w, r, gerr.Error(), gerr.Status())
			return
		}
		if f, err := clientACL.CheckPerm("read", opUser); err != nil {
			jsonErrorReport(w, r, err.Error(), err.Status())
			return
		} else if !f && !opUser.IsSelf(chefClient) {
			jsonErrorReport(w, r, "You are not allowed to perform that action.", http.StatusForbidden)
			return
		}

		/* API docs are wrong here re: public_key vs.
		 * certificate. Also orgname (at least w/ open source)
		 * and clientname, and it wants chef_type and
		 * json_class
		 */
		jsonClient := chefClient.ToJSON()
		enc := json.NewEncoder(w)
		if err := enc.Encode(&jsonClient); err != nil {
			jsonErrorReport(w, r, err.Error(), http.StatusInternalServerError)
			return
		}
	case "PUT":
		clientData, jerr := parseObjJSON(r.Body)
		if jerr != nil {
			jsonErrorReport(w, r, jerr.Error(), http.StatusBadRequest)
			return
		}
		chefClient, err := client.Get(org, clientName)
		if err != nil {
			jsonErrorReport(w, r, err.Error(), http.StatusNotFound)
			return
		}

		/* Makes chef-pedant happy. I suppose it is, after all,
		 * pedantic. */
		if averr := util.CheckAdminPlusValidator(clientData); averr != nil {
			jsonErrorReport(w, r, averr.Error(), averr.Status())
			return
		}

		clientACL, gerr := acl.GetItemACL(org, chefClient)
		if gerr != nil {
			jsonErrorReport(w, r, gerr.Error(), gerr.Status())
			return
		}
		f, err := clientACL.CheckPerm("read", opUser)
		if err != nil {
			jsonErrorReport(w, r, err.Error(), err.Status())
			return
		} else if !f && !opUser.IsSelf(chefClient) {
			jsonErrorReport(w, r, "You are not allowed to perform that action.", http.StatusForbidden)
			return
		}
		if !f {
			var verr util.Gerror
			aerr := opUser.CheckPermEdit(clientData, "admin")
			if !opUser.IsValidator() {
				verr = opUser.CheckPermEdit(clientData, "validator")
			}
			if aerr != nil && verr != nil {
				jsonErrorReport(w, r, "Client can be either an admin or a validator, but not both.", http.StatusBadRequest)
				return
			} else if aerr != nil || verr != nil {
				if aerr == nil {
					aerr = verr
				}
				jsonErrorReport(w, r, aerr.Error(), aerr.Status())
				return
			}
		}

		jsonName, sterr := util.ValidateAsString(clientData["name"])
		if sterr != nil {
			jsonErrorReport(w, r, sterr.Error(), http.StatusBadRequest)
			return
		}

		/* If clientName and clientData["name"] aren't the
		 * same, we're renaming. Check the new name doesn't
		 * already exist. */
		jsonClient := chefClient.ToJSON()
		if clientName != jsonName {
			if lerr := loginfo.LogEvent(org, opUser, chefClient, "modify"); lerr != nil {
				jsonErrorReport(w, r, lerr.Error(), http.StatusInternalServerError)
				return
			}
			err := chefClient.Rename(jsonName)
			if err != nil {
				jsonErrorReport(w, r, err.Error(), err.Status())
				return
			}
			w.WriteHeader(http.StatusCreated)
		}
		if uerr := chefClient.UpdateFromJSON(clientData); uerr != nil {
			jsonErrorReport(w, r, uerr.Error(), uerr.Status())
			return
		}

		if pk, pkfound := clientData["public_key"]; pkfound {
			switch pk := pk.(type) {
			case string:
				if pkok, pkerr := client.ValidatePublicKey(pk); !pkok {
					jsonErrorReport(w, r, pkerr.Error(), http.StatusBadRequest)
					return
				}
				chefClient.SetPublicKey(pk)
				jsonClient["public_key"] = pk
			case nil:
				//show_public_key = false

			default:
				jsonErrorReport(w, r, "Bad request", http.StatusBadRequest)
				return
			}
		}

		if p, pfound := clientData["private_key"]; pfound {
			switch p := p.(type) {
			case bool:
				if p {
					var cgerr error
					if jsonClient["private_key"], cgerr = chefClient.GenerateKeys(); cgerr != nil {
						jsonErrorReport(w, r, cgerr.Error(), http.StatusInternalServerError)
						return
					}
					// make sure the json
					// client gets the new
					// public key
					jsonClient["public_key"] = chefClient.PublicKey()
				}
			default:
				jsonErrorReport(w, r, "Bad request", http.StatusBadRequest)
				return
			}
		}
		chefClient.Save()
		if lerr := loginfo.LogEvent(org, opUser, chefClient, "modify"); lerr != nil {
			jsonErrorReport(w, r, lerr.Error(), http.StatusInternalServerError)
			return
		}

		enc := json.NewEncoder(w)
		if err := enc.Encode(&jsonClient); err != nil {
			jsonErrorReport(w, r, err.Error(), http.StatusInternalServerError)
			return
		}
	default:
		jsonErrorReport(w, r, "Unrecognized method for client!", http.StatusMethodNotAllowed)
	}
}

func clientListHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	vars := mux.Vars(r)
	org, orgerr := organization.Get(vars["org"])
	if orgerr != nil {
		jsonErrorReport(w, r, orgerr.Error(), orgerr.Status())
		return
	}
	clientResponse := make(map[string]string)
	opUser, oerr := actor.GetReqUser(org, r.Header.Get("X-OPS-USERID"))
	if oerr != nil {
		jsonErrorReport(w, r, oerr.Error(), oerr.Status())
		return
	}

	switch r.Method {
	case "GET":
		clientList := client.GetList(org)
		for _, k := range clientList {
			/* Make sure it's a client and not a user. */
			itemURL := util.JoinStr("/organizations/", org.Name, "/clients/", k)
			clientResponse[k] = util.CustomURL(itemURL)
		}
	case "POST":
		clientData, jerr := parseObjJSON(r.Body)
		if jerr != nil {
			jsonErrorReport(w, r, jerr.Error(), http.StatusBadRequest)
			return
		}
		if averr := util.CheckAdminPlusValidator(clientData); averr != nil {
			jsonErrorReport(w, r, averr.Error(), averr.Status())
			return
		}
		/*
		if !opUser.IsAdmin() && !opUser.IsValidator() {
			jsonErrorReport(w, r, "You are not allowed to take this action.", http.StatusForbidden)
			return
		} else if !opUser.IsAdmin() && opUser.IsValidator() {
			if aerr := opUser.CheckPermEdit(clientData, "admin"); aerr != nil {
				jsonErrorReport(w, r, aerr.Error(), aerr.Status())
				return
			}
			if verr := opUser.CheckPermEdit(clientData, "validator"); verr != nil {
				jsonErrorReport(w, r, verr.Error(), verr.Status())
				return
			}

		}
		*/
		clientACL, err := acl.Get(org, "containers", "clients")
		if err != nil {
			jsonErrorReport(w, r, err.Error(), err.Status())
			return
		} 
		if f, err := clientACL.CheckPerm("create", opUser); err != nil {
			jsonErrorReport(w, r, err.Error(), err.Status())
			return
		} else if !f && !opUser.IsValidator() { 
		// may need an org assoc check with the validator, although if
		// the client was found in this org it must be OK.
			jsonErrorReport(w, r, "You are not allowed to perform that action", http.StatusForbidden)
			return
		}
		clientName, sterr := util.ValidateAsString(clientData["name"])
		if sterr != nil || clientName == "" {
			jsonErrorReport(w, r, "Field 'name' missing", http.StatusBadRequest)
			return
		}

		chefClient, err := client.NewFromJSON(org, clientData)
		if err != nil {
			jsonErrorReport(w, r, err.Error(), err.Status())
			return
		}

		if publicKey, pkok := clientData["public_key"]; !pkok {
			var perr error
			if clientResponse["private_key"], perr = chefClient.GenerateKeys(); perr != nil {
				jsonErrorReport(w, r, perr.Error(), http.StatusInternalServerError)
				return 
			}
		} else {
			switch publicKey := publicKey.(type) {
			case string:
				if pkok, pkerr := client.ValidatePublicKey(publicKey); !pkok {
					jsonErrorReport(w, r, pkerr.Error(), pkerr.Status())
					return
				}
				chefClient.SetPublicKey(publicKey)
			case nil:

				var perr error
				if clientResponse["private_key"], perr = chefClient.GenerateKeys(); perr != nil {
					jsonErrorReport(w, r, perr.Error(), http.StatusInternalServerError)
					return
				}
			default:
				jsonErrorReport(w, r, "Bad public key", http.StatusBadRequest)
				return 
			}
		}
		/* If we make it here, we want the public key in the
		 * response. I think. */
		clientResponse["public_key"] = chefClient.PublicKey()

		chefClient.Save()
		if lerr := loginfo.LogEvent(org, opUser, chefClient, "create"); lerr != nil {
			jsonErrorReport(w, r, lerr.Error(), http.StatusInternalServerError)
			return 
		}
		clientResponse["uri"] = util.ObjURL(chefClient)
		w.WriteHeader(http.StatusCreated)
	default:
		jsonErrorReport(w, r, "Method not allowed for clients or users", http.StatusMethodNotAllowed)
		return
	}
	enc := json.NewEncoder(w)
	if err := enc.Encode(&clientResponse); err != nil {
		jsonErrorReport(w, r, err.Error(), http.StatusInternalServerError)
	}
}

